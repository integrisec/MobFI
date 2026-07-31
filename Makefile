BINARY := mfi
BIN_DIR := bin

# Version is canonical in internal/version; builds only stamp the git commit
# and date on top of it. Override VERSION_PKG for a fork of the module path.
VERSION_PKG := github.com/integrisec/MobFI/internal/version
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null)
DATE        := $(shell date -u +%Y-%m-%d)
LDFLAGS     := -X $(VERSION_PKG).Commit=$(COMMIT) -X $(VERSION_PKG).Date=$(DATE)

.PHONY: setup build test vet fmt run tidy clean version check-ascii \
        handbook handbook-check hooks

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

# Regenerate docs/handbook.{md,pdf} from the chapter sources in
# docs/handbook/. The pre-commit hook does this automatically when a
# chapter changes; run it by hand after editing without committing.
handbook:
	./scripts/build-handbook.sh

# Verify the committed handbook matches its sources. Wired into CI so a
# chapter edit committed without regenerating fails the build.
handbook-check:
	./scripts/build-handbook.sh --check

# Point git at the repo's hooks directory (one-time, per clone).
# Enables handbook auto-regeneration on commit.
hooks:
	git config core.hooksPath .githooks
	@echo "git hooks enabled from .githooks/"

fmt:
	gofmt -l -w .

run:
	go run ./cmd/mfi

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
