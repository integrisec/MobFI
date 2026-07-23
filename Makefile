BINARY := mfi
BIN_DIR := bin

.PHONY: build test vet fmt run tidy clean

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
