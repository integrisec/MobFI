BINARY := mfi
BIN_DIR := bin

.PHONY: setup build test vet fmt run tidy clean

# One-shot bootstrap for newcomers: installs Go, Wails, adb and
# libimobiledevice, then builds the CLI and GUI (macOS/Linux).
setup:
	./scripts/install.sh

build:
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/mfi

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

run:
	go run ./cmd/mfi

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
