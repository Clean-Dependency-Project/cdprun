# lineaje-sbom - Standalone CLI (no cdp-tbd dependency)
# Cross-platform, CGO_ENABLED=0 for static binaries (no libc).

BINARY_NAME := lineaje-sbom
DIST_DIR := dist

.PHONY: build build-all test lint run clean

build:
	CGO_ENABLED=0 go build -o $(BINARY_NAME) -ldflags="-s -w" .

build-all: clean
	mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 -ldflags="-s -w" .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 -ldflags="-s -w" .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 -ldflags="-s -w" .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe -ldflags="-s -w" .

test:
	go test ./...

lint:
	go vet ./...
	@which golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || true

run: build
	./$(BINARY_NAME) --help

clean:
	rm -f $(BINARY_NAME)
	rm -rf $(DIST_DIR)
