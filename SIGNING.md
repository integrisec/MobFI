# MobFI release signing

Two of the security fixes on the `security-audit-2026-07-31`
branch introduce hard build- and release-time dependencies:

- `MFI-UPD-01` -- the binary self-update path
  (`internal/selfupdate.applyBinary`) now verifies an ed25519
  signature over the release's `SHA256SUMS.txt`. Builds that
  ship without an embedded pubkey **refuse to install any
  update**. Releases that ship without a `SHA256SUMS.sig`
  asset **cannot be self-installed** by clients.
- `MFI-UPD-03` -- the git self-update path
  (`internal/selfupdate.applyGit`) now runs
  `git verify-commit HEAD` after `git pull`. Pulls where HEAD
  is not signed by a key in the operator's gpg / ssh trust
  set **abort before running `install.sh` / `install.ps1`**.

Neither infrastructure exists in the current repo. Do NOT ship
a release cut from this branch until the steps below are in
place -- clients will silently reject every self-update
attempt.

This file is the checklist for wiring both up.

## 1. Generate the release-signing keypair

Use ed25519 (small, fast, no PKI). Keep the private key **on
an offline machine or in a hardware token**; the public half
gets baked into every MobFI binary at build time.

```
# Generate the keypair (produces two files, keep .private OFFLINE)
openssl genpkey -algorithm ed25519 -out mobfi-release.private
openssl pkey -in mobfi-release.private -pubout -out mobfi-release.public

# Emit the raw 32-byte pubkey as base64 -- this is what MobFI needs
# to embed at build time. Strip the DER wrapper and encode the last
# 32 bytes:
openssl pkey -in mobfi-release.public -pubin -outform DER \
  | tail -c 32 \
  | base64 -w0 > mobfi-release.pubkey.b64
cat mobfi-release.pubkey.b64
```

Also acceptable and simpler operationally: `minisign -G -p
mobfi-release.pub -s mobfi-release.sec` (uses ed25519
internally). Extract the raw 32-byte key from the minisign
file if you go this route.

Store `mobfi-release.private` in a password manager or on a
hardware token (YubiKey, SoloKey, Nitrokey). Losing it means
regenerating and rebuilding every distributed binary.

## 2. Build MobFI with the pubkey baked in

Every release build must inject the base64 pubkey at link
time. Add to your release workflow:

```
PUBKEY="$(cat mobfi-release.pubkey.b64)"
LDFLAGS="-X 'github.com/integrisec/MobFI/internal/selfupdate.pubKeyBase64=${PUBKEY}'"

go build -ldflags "${LDFLAGS}" ./cmd/mfi
go build -ldflags "${LDFLAGS}" ./cmd/mfi-gui   # or the wails-build variant
```

`scripts/install.sh` and `scripts/install.ps1` will need the
same ldflags treatment, either by taking `MOBFI_PUBKEY_B64`
from the environment and constructing the `-ldflags` string,
or by hardcoding the pubkey in a `signing.txt` the scripts
read.

The recommended approach is: read the pubkey from a repo file
`.mobfi-pubkey.b64` that both `install.sh` and CI can source;
that file is committed to the repo, so anyone can rebuild
against the same pubkey, but the corresponding **private key
never leaves the offline machine**.

## 3. Publish `SHA256SUMS.sig` alongside every release

The release-cut workflow already emits `SHA256SUMS.txt`. Add
one more step that signs it with the offline private key and
publishes the signature as `SHA256SUMS.sig` in the same
GitHub release:

```
# On the release-signing machine (offline preferred), sign the
# exact bytes of SHA256SUMS.txt with the private key generated
# in step 1:
openssl pkeyutl -sign -inkey mobfi-release.private \
  -rawin -in SHA256SUMS.txt \
  -out SHA256SUMS.sig.raw

# MobFI's parseSignature accepts either raw 64 bytes or base64.
# Publish base64 -- friendlier to eyeball / diff:
base64 -w0 SHA256SUMS.sig.raw > SHA256SUMS.sig
```

Then upload `SHA256SUMS.sig` to the release via `gh release
upload` (or the workflow equivalent) alongside
`SHA256SUMS.txt` and every binary asset.

MobFI's `internal/selfupdate.Check` locates the signature by
looking for a release asset named exactly `SHA256SUMS.sig`.
Rename mismatches will make `applyBinary` abort with "release
does not publish SHA256SUMS.sig".

## 4. Sign every commit that lands on `main`

`applyGit` runs `git verify-commit HEAD` after pulling. Every
maintainer who pushes to `main` therefore needs to sign their
commits with a key the operator (the person running MobFI's
"Update now") has imported and trusted.

Two accepted paths:

**gpg-signed commits:**

```
# Maintainer side (one-time)
gpg --full-generate-key                     # pick ed25519
git config --global user.signingkey <KEYID>
git config --global commit.gpgsign true

# Publish the pubkey (attach to a GitHub Release, put in a repo
# file .maintainer-keys.gpg, or list on your website).

# Operator side (one-time)
gpg --import .maintainer-keys.gpg
gpg --lsign-key <KEYID>                     # mark as locally trusted
```

**ssh-signed commits (modern alternative, no gpg required):**

```
# Maintainer side (one-time)
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/id_ed25519.pub
git config --global commit.gpgsign true

# Publish the ssh pubkey (list in .allowed-signers with the
# commit gpg.ssh.allowedSignersFile config):
echo 'maintainer@example.com ssh-ed25519 AAAA...' > .allowed-signers

# Operator side (one-time)
git config gpg.ssh.allowedSignersFile .allowed-signers
```

Either way, `git verify-commit HEAD` will now succeed on any
commit signed by the maintainer, and applyGit will proceed to
the rebuild.

**If you cannot roll out signing right away**, either revert
`security: MFI-UPD-03` on the branch, or add a temporary
`insecure_git_updates` build tag around the `verify-commit`
call. Do not ship without one of those in place -- the
git-update path will 100% error out on the first user click.

## 5. Rotate

- **Release keys (ed25519, step 1)**: rotate by generating a
  new key, rebuilding all binaries with the new pubkey, and
  publishing signed checksums with both old and new keys for
  one release cycle. `internal/selfupdate.pubKeyBase64` is a
  single-key slot today; multi-key trust requires code
  changes.
- **Maintainer signing keys (step 4)**: standard git rotation
  -- add the new pubkey to `.allowed-signers` or the trusted
  gpg keyring; revoke the old key at your leisure.

## 6. What breaks until this is done

- `mfi update` (CLI) and "Update now" (GUI) both abort with
  `release-signing public key is not configured in this
  build` (binary path) or `git verify-commit HEAD failed`
  (git path).
- `Check()` still runs -- version-availability notice works
  as before. Only the `Apply` step is affected.
- All non-update functionality (extract / scan / diff / db /
  render / decode / keys / console) is unaffected.

## Cross-references

- `SECURITY-AUDIT.md` findings MFI-UPD-01 and MFI-UPD-03.
- `SECURITY-FIXES.md` fix rows for those IDs and their
  commit SHAs.
- `internal/selfupdate/apply.go` -- `pubKeyBase64` var
  (top of file), `loadPubKey`, `verifyChecksums`,
  `applyGit` `git verify-commit HEAD`.
