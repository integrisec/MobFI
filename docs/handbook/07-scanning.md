# Scanning for secrets

The scanner walks an extracted tree and reports strings matching a
catalog of credential patterns: cloud keys, version-control and
package tokens, AI provider keys, payment keys, communication
webhooks, SaaS API keys, JWTs, PEM private keys, and connection
strings with embedded credentials.

```sh
$ mfi scan -root <extracted-tree>
```

```
3 finding(s)
  [aws-access-key-id] /home/op/cap/shared_prefs/aws.xml:4  AKIA...(20 chars)
  [jwt] /home/op/cap/databases/session.db:1  eyJh...(214 chars)
  [generic-secret-assignment] /home/op/cap/files/config.json:12  "api...(37 chars)
```

Each finding reports the rule that matched, the file and line, and a
**redacted fingerprint**: the first four characters plus the total
length. The raw value is never printed by `mfi scan`.

## Flags

| Flag | Meaning |
|---|---|
| `-root` | Extracted tree to scan (required) |
| `-known` | File of known secrets to also search for, one per line |
| `-verify` | Live-verify findings against each service's API (opt-in, network) |

## The rule catalog

Forty-four built-in rules, grouped by what they protect:

**Cloud and infrastructure**: AWS access key ids, GCP API keys, GCP
OAuth client secrets, GCP service-account key ids, DigitalOcean
tokens, Databricks tokens, Doppler tokens, Terraform Cloud tokens.

**Version control and packages**: GitHub tokens (classic and
fine-grained), GitLab personal access tokens, npm tokens, PyPI
tokens, Postman API keys.

**AI providers**: OpenAI, Anthropic, Hugging Face.

**Payments**: Stripe secret and restricted keys, Square access
tokens, Braintree access tokens.

**Communication and email**: Slack tokens, app tokens and webhooks,
Discord bot tokens and webhooks, Telegram bot tokens, Twilio API
keys, SendGrid, Mailgun, Mailchimp.

**SaaS APIs**: Shopify, Notion, Linear, Airtable, New Relic,
Grafana.

**Generic and structural**: JWTs, PEM private key headers, MongoDB
and SQL and Redis connection URIs with embedded credentials, HTTP
basic-auth URLs, `key = "value"` style secret assignments, and
`Bearer` tokens.

Every pattern is anchored on the issuer's published token format
(the public prefix, character alphabet, and length), which keeps
false positives low. Patterns compile under Go's RE2 engine, so no
input can trigger catastrophic backtracking.

### The generic rules

`generic-secret-assignment` and `bearer-token` are deliberately
broader than the vendor-specific rules. They catch credentials for
services with no distinctive token format, at the cost of matching
some non-secrets (a config key literally named `password` holding a
placeholder, for instance). Expect to triage these by hand. The
vendor-specific rules are high-confidence; the generic pair are
leads.

## Known-secret lists

When you already hold a credential and want to know whether it
appears in the app's data, put it in a file (one per line, blank
lines and `#` comments ignored) and pass it in:

```sh
$ cat known.txt
# Credentials issued for this engagement
hunter2SuperSecretValue
sk_test_51H9xExampleKeyMaterial

$ mfi scan -root ./cap -known known.txt
```

Matches report under the rule id `known-secret`. Literal values are
regex-quoted before use, so metacharacters in your secrets are
matched literally and cannot corrupt the scanner's patterns.

Use this to answer questions like:

- Does the app persist the session token issued at login?
- Did the credential I typed into the login form end up in
  plaintext on disk?
- Is the API key from the app's build config also present at
  runtime?

## What the scanner skips

| Limit | Value | Rationale |
|---|---|---|
| Max file size | 16 MB | Larger files are skipped entirely |
| Max line length | 1 MB | A file with a longer line is abandoned |
| Max matches per rule per line | 50 | Prevents one pathological line dominating |
| Binary detection | First 512 bytes | A NUL byte in the sniff window marks the file binary and skips it |

The binary skip matters for interpretation: **a secret embedded in a
compiled binary, an image, or a compressed archive will not be
found**. The scanner works on text. If you suspect a credential in a
binary artifact, extract the strings yourself and scan that output,
or use the Decode tab on a suspicious blob.

## Live verification

`-verify` answers a different question from "is there a
credential-shaped string here": it answers **is this credential
still live**.

```sh
$ mfi scan -root ./cap -verify
verifying findings against their services...
2 finding(s)
  [github-token] /home/op/cap/files/config.json:8  ghp_...(40 chars)  [active]
  [stripe-secret-key] /home/op/cap/files/config.json:9  sk_l...(107 chars)  [inactive]
```

Statuses: `active`, `inactive`, `unknown` (the check could not
complete), and `unsupported` (the rule has no verifier).

**This sends the matched secret to the service that issued it.**
Read that sentence twice before using the flag on a client
engagement. Every verifier calls a read-only "whoami"-style endpoint
over HTTPS: nothing is created, modified, or deleted. But the
credential does leave your workstation, and the request appears in
the service's audit log, attributable to your source IP and the
time you ran it.

When to use it:

- The finding count is large and you need to prioritise triage.
- You need to demonstrate real impact ("this key is live") rather
  than theoretical exposure.
- The client has authorised outbound verification in scope.

When not to use it:

- The engagement forbids outbound traffic to third parties.
- The credentials belong to a third party that has not consented.
- You are working on an air-gapped or otherwise contained host.

Identical secrets are verified once, no matter how many files they
appear in. Verification runs with a bounded concurrency and a short
per-request timeout, so a hanging provider does not stall the scan
indefinitely.

Rules with verifiers cover the major providers (GitHub, GitLab, AWS,
OpenAI, Anthropic, Stripe, Slack, Postman, Notion, Airtable);
everything else reports `unsupported`.

## Interpreting findings

A finding is a lead, not a conclusion. Triage in this order:

1. **Is it real?** Open the file and look at the context. `mfi
   render -file <path>` shows it in a readable form. Placeholders,
   example keys from documentation, and test fixtures all match
   real patterns.
2. **Is it reachable?** A credential in an app's private data
   directory is only exposed to an attacker who can already read
   that directory: another app with root, a device thief with an
   unlocked handset, a malicious backup consumer. Say which threat
   model applies.
3. **Is it live?** Either `-verify`, or manual validation against
   the service if outbound verification is out of scope.
4. **What does it grant?** A read-only analytics key and a
   production payments key are the same shape and wildly different
   findings.

## Where the raw values are

`mfi scan` never prints raw secrets. To retrieve them:

- **CLI**: `mfi report -root <tree> -show-secrets` includes raw
  values in the report. Do not share that output.
- **GUI**: click a redacted value in the Scan tab to reveal it, or
  use the row's Copy action.

## In the GUI

The Scan tab adds:

- A **progress indicator** with file counts, and a cancel button.
- **Sortable columns** by rule, path, line, and verification status.
- **Click-to-reveal** on the redacted match column.
- Per-row actions: **Render** (open the file with the secret
  highlighted), **Decode** (send the value to the Decode tab), and
  **Copy**.
- A **Live verify** checkbox, gated behind a confirmation dialog
  that states plainly what the verification does.

<!-- screenshot: scan-tab-findings.png -->

## Next

- Diffing: what changed between two captures.
- Reporting: turn findings into a shareable artifact.
