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
