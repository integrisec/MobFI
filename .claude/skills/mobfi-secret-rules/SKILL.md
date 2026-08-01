---
name: mobfi-secret-rules
description: >
  Add, modify, or review a secret-detection rule in MobFI's scanner, and the
  optional live verifier that checks whether a matched credential is still
  active. Triggers on "add a detection rule", "MobFI does not detect <vendor>
  tokens", "add a secret pattern", "this rule false-positives", "add a
  verifier", "the scanner missed X", or any edit under internal/secrets.
  Covers the RE2 constraint, anchoring a pattern on a published token format,
  where the rule goes in the grouped catalog, the redaction invariant,
  verifier safety rules (read-only endpoints, no secret in a URL), and the
  test fixtures a new rule needs. Does not cover report formatting
  (internal/report) or the GUI scan tab (mobfi-gui-binding).
---

# Secret detection rules

## Purpose

Extend or correct `internal/secrets` without introducing false
positives, unsafe regex behaviour, or a verifier that leaks the
credential it is checking.

## Files

| File | Contents |
|---|---|
| `internal/secrets/secrets.go` | `Rule`, `Scanner`, the built-in catalog, redaction, known-secret loading |
| `internal/secrets/verify.go` | `VerifyStatus`, the verifier map, per-provider verifiers |
| `internal/secrets/secrets_test.go` | Scanner and redaction tests |

## Adding a rule

A rule is an id plus a compiled pattern:

```go
mustRule("vendor-token-kind", `\bprefix_[0-9A-Za-z]{32}\b`),
```

Append it to `builtinRules` **in the correct group**. The catalog is
organised by what the credential protects, with comment banners:
cloud and infrastructure, version control and packages, AI
providers, payments, communication and email, SaaS APIs, generic and
structural, connection URIs. Put the rule with its peers; a reader
scanning for "do we cover Vendor X" looks by category.

### Naming

`<vendor>-<credential-kind>`, lowercase, hyphenated:
`github-token`, `github-fine-grained-pat`, `stripe-secret-key`,
`gcp-service-account-key`. The id appears in every finding, every
report, and every user's grep, so it is API. Do not rename an
existing one without a reason.

### Writing the pattern

**RE2 only.** Go's `regexp` has no backtracking, no lookaround, and
no backreferences. Verified behaviour:

```
(?=foo)bar    -> error parsing regexp: invalid or unsupported Perl syntax: `(?=`
(?<=foo)bar   -> error parsing regexp: invalid named capture
(a)\1         -> error parsing regexp: invalid escape sequence: `\1`
```

That is a feature, not a limitation to work around: it means no
input can trigger catastrophic backtracking, so the scanner is safe
against a hostile file that would hang a PCRE engine. Do not
introduce a regex library that restores those constructs.

**Anchor on the issuer's published format.** Every good rule keys on
a documented prefix plus a documented length:

```go
mustRule("github-token", `\b(?:ghp|gho|ghu|ghs|ghr)_[0-9A-Za-z]{36}\b`),
mustRule("stripe-secret-key", `\b(?:sk|rk)_(?:live|test)_[0-9A-Za-z]{24,}\b`),
mustRule("slack-webhook", `https://hooks\.slack\.com/services/T[0-9A-Za-z_]+/B[0-9A-Za-z_]+/[0-9A-Za-z_]+`),
```

Find the vendor's own documentation for the token format. Do not
copy a pattern out of another scanner: match the documented format,
not someone else's expression of it.

**Use `\b` word boundaries** so a token embedded in a longer
alphanumeric run does not match.

**Prefer exact lengths over open-ended quantifiers.** `{36}` beats
`{20,}`: it rejects the near-misses that generate triage load. Use a
range only where the vendor genuinely varies.

**Escape literal dots** in hostnames (`hooks\.slack\.com`). An
unescaped dot matches any character and quietly widens the rule.

### The precision bar

The catalog is high-signal by design. Before adding a rule, decide
which kind it is.

**Vendor-specific, high confidence.** A distinctive prefix and a
known length; it should essentially never match a non-secret. This
is the default and what most rules are.

**Generic, lead-generating.** `generic-secret-assignment` and
`bearer-token` are deliberately broad and will match placeholders,
documentation examples, and test fixtures. There are exactly two,
and that is enough. Do not add a third without a strong case: each
one multiplies triage load across every scan every user runs.

A rule that cannot meet the first bar and is not clearly worth the
second does not belong in the built-in catalog. Point the user at a
known-secret list instead (`-known`, one literal per line), which is
the supported mechanism for engagement-specific values.

## Testing a rule

Add coverage in `internal/secrets/secrets_test.go` following the
existing shape: a fixture constant, a temp-dir tree, an assertion on
rule id and redaction.

```go
const vendorToken = "prefix_" + "..." // exactly the documented length
```

Test three things:

1. **It matches** a correctly-formatted token.
2. **It does not match** a near-miss: one character short, one
   character long, wrong prefix, embedded in a longer run.
3. **The finding is redacted**: `Match` is the fingerprint, and raw
   values do not appear where they should not.

The near-miss test is the one that catches a sloppy quantifier.

```sh
go test ./internal/secrets/
go test ./internal/secrets/ -run TestScanTree
```

## Scanner limits

Know these before accepting a report that the scanner "missed"
something:

| Limit | Value | Consequence |
|---|---|---|
| `defaultMaxFileSize` | 16 MB | Larger files are skipped whole |
| `maxLineLen` | 1 MB | A file with a longer line is abandoned |
| `maxMatchesPerLine` | 50 | Per rule, per line |
| `sniffLen` | 512 bytes | A NUL in the sniff window marks the file binary and skips it |

**Secrets inside compiled binaries, images, and archives are not
found**, by design: this is a text scanner. A report that a token in
a `.so` was missed is working as intended. Changing that is a
scanner-architecture decision, not a rule change.

## The redaction invariant

`Finding.Match` carries a redacted fingerprint: the first four
characters plus the length. `Finding.Secret` carries the raw value
so the operator can reveal or copy it deliberately.

**Anything that leaves the process by default shows `Match`, never
`Secret`.** `report.BuildWith` strips `Secret` unless `unredacted`
is explicitly set, and the CLI's `runScan` prints `f.Match`.

When you add a code path that emits findings, decide which side of
that line it is on, and default to `Match`.

For vendor rules the four leading characters are the public prefix
(`ghp_`, `AKIA`), so the fingerprint leaks nothing. That is only
true because rules are prefix-anchored: a rule matching a
high-entropy string with no prefix would leak four real characters
of the secret. One more reason to anchor on a documented format.

## Adding a verifier

A verifier answers "is this credential live" by calling the issuing
service. It is opt-in per scan (`-verify`), because it sends the
credential off the workstation.

```go
var verifiers = map[string]verifier{
    "vendor-token-kind": vendorVerify,
}

func vendorVerify(ctx context.Context, client *http.Client, secret string) VerifyStatus {
    ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
    defer cancel()
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.vendor.com/v1/me", nil)
    if err != nil {
        return VerifyUnknown
    }
    req.Header.Set("Authorization", "Bearer "+secret)
    resp, err := client.Do(req)
    if err != nil {
        return VerifyUnknown
    }
    defer resp.Body.Close()
    switch resp.StatusCode {
    case http.StatusOK:
        return VerifyActive
    case http.StatusUnauthorized, http.StatusForbidden:
        return VerifyInactive
    default:
        return VerifyUnknown
    }
}
```

### Verifier rules, all mandatory

**Read-only endpoint only.** A "whoami", "current user", or "list
account" call. Never anything that creates, modifies, or deletes:
the tool is pointed at credentials belonging to someone who did not
consent to state changes.

**HTTPS, always.** No exceptions, no plaintext fallback.

**Never put the secret in a URL.** Query strings land in server
access logs, proxy logs, and referrer headers. Use a header or a
POST body.

**Return `VerifyUnknown` for anything ambiguous:** network error,
timeout, 500, unexpected status. `VerifyInactive` is a positive
claim that the credential is dead, and a wrong claim there gets a
real finding dismissed. Reserve it for an explicit auth rejection.

**Use the passed-in `client` and `verifyTimeout`.** Do not construct
your own `http.Client`: the shared one carries the timeout and
transport configuration the whole verifier set depends on.

**Use the exact documented hostname.** A typo'd host sends the
credential to whoever registered that domain.

### Verifier checklist

- [ ] Endpoint is read-only and documented by the vendor
- [ ] HTTPS, correct hostname, no secret in the URL
- [ ] Uses the passed-in `client` and `verifyTimeout`
- [ ] Ambiguous outcomes return `VerifyUnknown`
- [ ] Registered in the `verifiers` map under the exact rule id
- [ ] Rules without a verifier still report `VerifyUnsupported`

## Documentation

`docs/handbook/07-scanning.md` lists the rule groups and states the
scanner's limits. Adding a vendor to a group, or changing a limit,
means updating that chapter in the same pull request. CI checks the
generated handbook is in sync with its sources.

## Cross-references

- `mobfi-untrusted-input`: the scanner reads attacker-controlled
  files; that skill covers the discipline for device-facing code.
- `mobfi-gui-binding`: the Scan tab's reveal, copy, and verify
  affordances.
- `mobfi-architecture`: where `internal/secrets` sits.
