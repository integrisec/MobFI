package secrets

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// VerifyStatus is the outcome of a live verification attempt for a finding.
type VerifyStatus string

const (
	// VerifyUnattempted means verification was not run (the default).
	VerifyUnattempted VerifyStatus = ""
	// VerifyUnsupported means no live verifier exists for the finding's rule.
	VerifyUnsupported VerifyStatus = "unsupported"
	// VerifyActive means the service confirmed the credential is valid/live.
	VerifyActive VerifyStatus = "active"
	// VerifyInactive means the service rejected the credential (revoked/invalid).
	VerifyInactive VerifyStatus = "inactive"
	// VerifyUnknown means the verifier ran but was inconclusive (network error,
	// rate limit, unexpected response).
	VerifyUnknown VerifyStatus = "unknown"
)

const (
	verifyTimeout     = 8 * time.Second
	verifyConcurrency = 8
	// maxPerHost caps total verifications sent to any one vendor per Verify
	// call. Attacker seeds an app with N format-valid but fake tokens; without
	// a cap Verify fires N GETs at that vendor and the operator's IP hits the
	// vendor's abuse detection floor. See MFI-SEC-04.
	maxPerHost = 50
	// perHostGap paces requests to a single vendor host so a burst of many
	// findings for the same rule does not trigger rate-limiting even below
	// maxPerHost. See MFI-SEC-04.
	perHostGap = 250 * time.Millisecond
)

// verifierHosts pairs each verifier rule with the vendor host it calls, so
// the pacer can rate-limit per host regardless of which rule triggered.
// Kept in sync manually with the verifiers table; a wrong entry only means
// pacing is less accurate, never a functional bug.
var verifierHosts = map[string]string{
	"github-token":            "api.github.com",
	"github-fine-grained-pat": "api.github.com",
	"gitlab-token":            "gitlab.com",
	"aws-access-key-id":       "sts.amazonaws.com",
	"openai-api-key":          "api.openai.com",
	"stripe-secret-key":       "api.stripe.com",
	"slack-token":             "slack.com",
	"postman-api-key":         "api.getpostman.com",
	"notion-token":            "api.notion.com",
	"airtable-token":          "api.airtable.com",
	"anthropic-api-key":       "api.anthropic.com",
}

// verifier confirms whether a matched secret is live by calling its service's
// API. It must never modify anything on the service -- read-only "whoami"-style
// endpoints only.
type verifier func(ctx context.Context, client *http.Client, secret string) VerifyStatus

// verifiers maps a rule ID to its live verifier. Only rules with a safe,
// read-only check are listed; everything else reports VerifyUnsupported.
var verifiers = map[string]verifier{
	"github-token":            githubVerify,
	"github-fine-grained-pat": githubVerify,
	"gitlab-pat":              gitlabVerify,
	"npm-token":               bearerGet("https://registry.npmjs.org/-/whoami"),
	"openai-api-key":          bearerGet("https://api.openai.com/v1/models"),
	"anthropic-api-key":       anthropicVerify,
	"huggingface-token":       bearerGet("https://huggingface.co/api/whoami-v2"),
	"sendgrid-api-key":        bearerGet("https://api.sendgrid.com/v3/scopes"),
	"digitalocean-token":      bearerGet("https://api.digitalocean.com/v2/account"),
	"stripe-secret-key":       stripeVerify,
	"slack-token":             slackVerify,
	"postman-api-key":         postmanVerify,
	"notion-token":            notionVerify,
	"airtable-token":          bearerGet("https://api.airtable.com/v0/meta/whoami"),
}

// Verifiable reports whether a rule has a live verifier.
func Verifiable(ruleID string) bool { _, ok := verifiers[ruleID]; return ok }

// Verify performs LIVE verification of findings that have a verifier, setting
// each Finding.Verified. It makes authenticated NETWORK CALLS to each service
// with the matched secret, so it is strictly opt-in. Identical secrets are
// checked once. Findings whose rule has no verifier are marked
// VerifyUnsupported; the returned slice is the same one passed in (mutated).
func Verify(ctx context.Context, findings []Finding) []Finding {
	// MFI-XC-02: never route verification through the environment's HTTPS
	// proxy. Every request carries a live client secret; a corporate MITM
	// proxy on an operator's laptop would otherwise silently receive every
	// discovered token during a client engagement.
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	client := &http.Client{
		Timeout:   verifyTimeout,
		Transport: transport,
		// MFI-SEC-02: whoami endpoints do not legitimately redirect. Go's
		// default CheckRedirect follows up to 10 hops and strips only
		// Authorization / Cookie on cross-origin -- custom vendor headers
		// (PRIVATE-TOKEN, x-api-key, X-Api-Key) would ride through to a
		// redirect target. Treating any redirect as an error keeps a
		// discovered token from leaking to an attacker-controlled host.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	type key struct{ rule, secret string }
	uniq := map[key]VerifyStatus{}
	for i := range findings {
		if _, ok := verifiers[findings[i].RuleID]; !ok {
			findings[i].Verified = VerifyUnsupported
			continue
		}
		if findings[i].Secret == "" {
			continue // nothing to verify (redacted before reaching us)
		}
		uniq[key{findings[i].RuleID, findings[i].Secret}] = VerifyUnattempted
	}

	keys := make([]key, 0, len(uniq))
	for k := range uniq {
		keys = append(keys, k)
	}

	// MFI-SEC-04: per-host pacer + per-host request cap. hostCount tracks
	// how many verifications we have attempted per host so far, and
	// hostNextAvailable holds the wall-clock time the next request to each
	// host may go out. Excess requests to a host mark the finding as
	// VerifyUnknown rather than firing.
	var mu sync.Mutex
	hostCount := map[string]int{}
	hostNextAvailable := map[string]time.Time{}
	waitForHost := func(host string) bool {
		mu.Lock()
		if hostCount[host] >= maxPerHost {
			mu.Unlock()
			return false
		}
		hostCount[host]++
		earliest := hostNextAvailable[host]
		nextSlot := time.Now()
		if earliest.After(nextSlot) {
			nextSlot = earliest
		}
		hostNextAvailable[host] = nextSlot.Add(perHostGap)
		mu.Unlock()
		if delay := time.Until(nextSlot); delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return false
			}
		}
		return true
	}

	sem := make(chan struct{}, verifyConcurrency)
	var wg sync.WaitGroup
	for _, k := range keys {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(k key) {
			defer wg.Done()
			defer func() { <-sem }()
			host := verifierHosts[k.rule]
			if host != "" && !waitForHost(host) {
				mu.Lock()
				uniq[k] = VerifyUnknown
				mu.Unlock()
				return
			}
			st := verifiers[k.rule](ctx, client, k.secret)
			mu.Lock()
			uniq[k] = st
			mu.Unlock()
		}(k)
	}
	wg.Wait()

	for i := range findings {
		if _, ok := verifiers[findings[i].RuleID]; ok && findings[i].Secret != "" {
			findings[i].Verified = uniq[key{findings[i].RuleID, findings[i].Secret}]
		}
	}
	return findings
}

// httpStatus performs the request and maps the HTTP status to a VerifyStatus:
// 2xx = active, 401 = inactive, anything else = unknown (conservative: a 403 or
// error is not treated as a confirmed revocation).
func httpStatus(ctx context.Context, client *http.Client, req *http.Request) VerifyStatus {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return VerifyUnknown
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return VerifyActive
	case resp.StatusCode == http.StatusUnauthorized:
		return VerifyInactive
	default:
		return VerifyUnknown
	}
}

// bearerGet verifies a token via a read-only GET with an Authorization: Bearer
// header -- the common shape for most token APIs.
func bearerGet(url string) verifier {
	return func(ctx context.Context, client *http.Client, secret string) VerifyStatus {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return VerifyUnknown
		}
		req.Header.Set("Authorization", "Bearer "+secret)
		return httpStatus(ctx, client, req)
	}
}

func githubVerify(ctx context.Context, client *http.Client, secret string) VerifyStatus {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return VerifyUnknown
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Accept", "application/vnd.github+json")
	return httpStatus(ctx, client, req)
}

func gitlabVerify(ctx context.Context, client *http.Client, secret string) VerifyStatus {
	req, err := http.NewRequest(http.MethodGet, "https://gitlab.com/api/v4/user", nil)
	if err != nil {
		return VerifyUnknown
	}
	req.Header.Set("PRIVATE-TOKEN", secret)
	return httpStatus(ctx, client, req)
}

func stripeVerify(ctx context.Context, client *http.Client, secret string) VerifyStatus {
	req, err := http.NewRequest(http.MethodGet, "https://api.stripe.com/v1/account", nil)
	if err != nil {
		return VerifyUnknown
	}
	req.SetBasicAuth(secret, "") // Stripe uses the secret key as the basic-auth username
	return httpStatus(ctx, client, req)
}

func anthropicVerify(ctx context.Context, client *http.Client, secret string) VerifyStatus {
	req, err := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return VerifyUnknown
	}
	req.Header.Set("x-api-key", secret)
	req.Header.Set("anthropic-version", "2023-06-01")
	return httpStatus(ctx, client, req)
}

func postmanVerify(ctx context.Context, client *http.Client, secret string) VerifyStatus {
	req, err := http.NewRequest(http.MethodGet, "https://api.getpostman.com/me", nil)
	if err != nil {
		return VerifyUnknown
	}
	req.Header.Set("X-Api-Key", secret)
	return httpStatus(ctx, client, req)
}

func notionVerify(ctx context.Context, client *http.Client, secret string) VerifyStatus {
	req, err := http.NewRequest(http.MethodGet, "https://api.notion.com/v1/users/me", nil)
	if err != nil {
		return VerifyUnknown
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Notion-Version", "2022-06-28")
	return httpStatus(ctx, client, req)
}

// slackVerify calls auth.test, which returns HTTP 200 with {"ok": false} for a
// bad token rather than a 401, so the JSON body must be inspected.
func slackVerify(ctx context.Context, client *http.Client, secret string) VerifyStatus {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/auth.test", nil)
	if err != nil {
		return VerifyUnknown
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := client.Do(req)
	if err != nil {
		return VerifyUnknown
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return VerifyUnknown
	}
	// MFI-SEC-05: cap the JSON body size so a redirect target / gateway
	// error page cannot stream unlimited data into the decoder within the
	// verify timeout window.
	if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		return VerifyUnknown
	}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return VerifyUnknown
	}
	if body.OK {
		return VerifyActive
	}
	return VerifyInactive
}
