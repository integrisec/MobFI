BINARY := mfi
BIN_DIR := bin

# Version is canonical in internal/version; builds only stamp the git commit
# and date on top of it. Override VERSION_PKG for a fork of the module path.
VERSION_PKG := github.com/integrisec/MobFI/internal/version
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null)
DATE        := $(shell date -u +%Y-%m-%d)
LDFLAGS     := -X $(VERSION_PKG).Commit=$(COMMIT) -X $(VERSION_PKG).Date=$(DATE)

.PHONY: setup build test vet fmt run tidy clean version check-ascii

# One-shot bootstrap for newcomers: installs Go, Wails, adb and
# libimobiledevice, then builds the CLI and GUI (macOS/Linux).
setup:
	./scripts/install.sh

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/mfi

version:
	@go run -ldflags "$(LDFLAGS)" ./cmd/mfi version

test:
	go test ./...

vet:
	go vet ./...

# Guard against non-ASCII bytes in PowerShell scripts (Windows PowerShell 5.1
# mis-parses them). Wired into CI; run locally with `make check-ascii`.
check-ascii:
	sh scripts/check-ascii.sh

fmt:
	gofmt -l -w .

run:
	go run ./cmd/mfi

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)

# --- Release engineering and quality gates -----------------------------------

.PHONY: dist fmt-check lint-sh vuln licenses

# Cross-compile the release assets + SHA256SUMS.txt into dist/, with the asset
# names the self-updater expects. See scripts/dist.sh --help; the signing
# steps that follow are documented in SIGNING.md and the script headers.
dist:
	./scripts/dist.sh

# Fail when any file needs gofmt (CI gates on this; `make fmt` rewrites).
fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then \
		echo "gofmt needed on:"; echo "$$out"; exit 1; \
	else echo "gofmt: clean"; fi

# Lint the shell scripts at the severity CI gates on (info-level notes are
# visible to a plain `shellcheck scripts/*.sh` but do not fail the build).
lint-sh:
	@command -v shellcheck >/dev/null 2>&1 || { \
		echo "shellcheck not installed (apt/dnf/brew install shellcheck)"; exit 1; }
	shellcheck --severity=warning scripts/*.sh

# Known-vulnerability reachability scan over the cgo-free core + CLI (CI-02).
# Run with a current Go toolchain: stdlib advisories are keyed to patch
# releases, so a stale toolchain reports its own already-fixed CVEs.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./cmd/mfi ./internal/...

# License policy gate over the whole dependency graph, GUI included (CI-03):
# fail on forbidden / restricted licenses before they ship.
licenses:
	go run github.com/google/go-licenses@v1.6.0 check ./... --disallowed_types=forbidden,restricted
