# Updating

MobFI checks for updates and can update itself in place, whether you
installed a prebuilt binary or built from a git checkout.

## Checking

```sh
$ mfi update
```

```
Current: v1.0.0
Latest:  v1.1.0

A newer release is available: v1.1.0
  https://github.com/integrisec/MobFI/releases/tag/v1.1.0
  Update now:  mfi update -apply
```

The check reports two independent signals:

- Whether a **newer published release** exists than the running
  version. This works for any install, including prebuilt binaries.
- When running from inside a **git checkout**, how many commits the
  local branch is behind its upstream.

Checking changes nothing on disk.

Machine-readable form:

```sh
$ mfi update -json
```

## Applying

```sh
$ mfi update -apply
```

What happens depends on how MobFI was installed:

**Git checkout**: `git pull --ff-only` from the public HTTPS URL,
then a rebuild via the project's install script. HTTPS rather than
the configured remote (often SSH) so an unattended update needs no
SSH key or agent.

**Prebuilt binary**: downloads the release asset for your platform,
verifies its SHA-256 against the published `SHA256SUMS.txt`, and
atomically replaces the running executable.

On Unix the replacement is a same-filesystem rename, which is atomic
even while the old binary runs. Windows cannot overwrite a running
image, so the old binary is moved aside first and rolled back if the
swap fails.

Re-run `mfi` afterwards to use the new version.

## Automatic notices

**One-shot subcommands** print a one-line notice to stderr after
running if an update is available, so a piped stdout stays clean:

```
Update available: MobFI v1.1.0 (you have v1.0.0) - run `mfi update` for details.
```

The notice is skipped for `update`, `version`, and `help`, where a
network check would be noise.

**The wizard** goes further: when stdin is an interactive terminal,
it offers to update at launch, applies it with live progress, and
re-execs the freshly-built binary so the wizard continues on the new
version. When stdin is piped, it prints the notice only.

**The GUI** checks in the background and shows a dismissable banner
with an "Update now" button. On macOS and Linux the update runs
in-process with progress in the window; on Windows the app closes,
a detached worker performs the swap in its own console window, and
the app relaunches automatically.

## Disabling the check

```sh
$ export MFI_NO_UPDATE_CHECK=1
```

Set this when:

- Working on an **air-gapped or contained host** where outbound
  traffic is not permitted.
- The engagement forbids **any unattributed outbound connection**
  from the testing workstation.
- You need **byte-identical tooling** across a test series and do
  not want a mid-engagement version change.

The check is a request to the GitHub releases API. It carries no
device data, no capture data, and no identifying information beyond
what any HTTPS client sends.

## Version pinning for an engagement

For work where reproducibility matters, pin the version at the start
and record it:

```sh
$ mfi version
mfi v1.0.0 (abc1234, 2026-07-31)

$ export MFI_NO_UPDATE_CHECK=1
```

Put the full version string in your engagement notes. If a finding
is later questioned, you can rebuild the exact tool that produced
it.

## Verifying an update

After updating, confirm the version and that the tools still
resolve:

```sh
$ mfi version
$ mfi doctor
```

A git-checkout update rebuilds from source, so a failed rebuild
leaves the previous binary in place; the error is reported and
nothing is silently half-installed.

## Manual update

If the self-updater cannot run (no network, restricted host,
policy), update by hand:

```sh
# Git checkout
$ git pull --ff-only
$ make build

# Prebuilt binary: download the new release, verify, replace
$ shasum -a 256 -c SHA256SUMS.txt --ignore-missing
$ mv mfi_v1.1.0_darwin_arm64 ~/.local/bin/mfi
```

If `scripts/install.sh` created a **symlink** into `~/.local/bin`
(the default), a `git pull` plus `make build` is picked up
automatically with no further steps.

## Next

Troubleshooting: when something does not behave.
