// Package sysproc builds exec.Cmd values that never flash a console window on
// Windows. A GUI process has no console of its own, so each child console
// program (adb, idevice*, aapt, ...) would otherwise pop a visible window --
// and MobFI polls devices on a timer, so those windows spawn continuously.
// On non-Windows platforms these are thin pass-throughs to os/exec.
package sysproc

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// Command is exec.Command, but hides the child's console window on Windows.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	configure(cmd)
	return cmd
}

// CommandContext is exec.CommandContext with the same Windows behaviour.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	configure(cmd)
	return cmd
}

// envAllowPrefixes are environment variable name prefixes that always pass
// through to child processes because a mobile-forensics tool needs them
// (PATH resolution, i18n, tool-specific config, agent forwarding when the
// operator has explicitly set it up).
var envAllowPrefixes = []string{
	"PATH", "HOME", "USER", "LOGNAME", "USERNAME", "USERPROFILE",
	"LANG", "LC_", "TERM", "DISPLAY", "XDG_",
	"TMPDIR", "TMP", "TEMP",
	"ADB_", "ANDROID_", "APPDATA", "LOCALAPPDATA", "PROGRAMFILES", "PROGRAMDATA",
	"SSH_AUTH_SOCK", "SSH_AGENT_PID",
	"SYSTEMROOT", "COMSPEC", "PATHEXT",
}

// envDenyPrefixes are secret-carrying environment variable name prefixes
// that must NEVER pass through to a child process, even if the operator
// has them set for other work in the same shell that launched MobFI.
// MFI-XC-05: adb / ssh / iproxy / idevicebackup2 / aapt inherited the full
// parent env, so a stray AWS_* or GITHUB_TOKEN in the operator's shell
// would flow into every device-side subprocess and end up in captured
// stderr, `~/.ssh/config` ProxyCommand exfiltrators, or debug dumps.
var envDenyPrefixes = []string{
	"AWS_", "GCP_", "GOOGLE_", "AZURE_",
	"GITHUB_TOKEN", "GH_TOKEN",
	"ANTHROPIC_", "OPENAI_", "COHERE_", "TOGETHER_",
	"SLACK_", "DISCORD_", "TELEGRAM_",
	"STRIPE_", "TWILIO_", "SENDGRID_", "MAILGUN_",
	"NPM_TOKEN", "PYPI_TOKEN", "GITLAB_TOKEN",
	"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "ALL_PROXY",
	"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
}

// CuratedEnv returns an env slice safe for passing to spawned tools:
// allowlisted prefixes (PATH, HOME, terminal / display, tool-specific)
// plus any extras the caller wants to add. Deny takes precedence over
// allow, so a variable like AWS_REGION (which starts with AWS_ in
// envDenyPrefixes) is dropped even though it is a public config value --
// the safe default is "if in doubt, drop it."
func CuratedEnv(extra ...string) []string {
	out := make([]string, 0, 32+len(extra))
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if hasAnyPrefix(name, envDenyPrefixes) {
			continue
		}
		if !hasAnyPrefix(name, envAllowPrefixes) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
