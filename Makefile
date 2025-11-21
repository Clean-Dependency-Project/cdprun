.PHONY: build test clean run run-auto

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=cdprun
BINARY_PATH=bin/$(BINARY_NAME)
ROOT_BINARY_NAME=cdprun
ROOT_BINARY_PATH=bin/$(ROOT_BINARY_NAME)

all: test build

build: test
	@echo "Building $(ROOT_BINARY_NAME)..."
	@$(GOBUILD) -o $(ROOT_BINARY_PATH) ./cmd/runtime-cli/main.go

build-only:
	@echo "Building $(ROOT_BINARY_NAME) (skipping tests)..."
	@mkdir -p bin
	@$(GOBUILD) -o $(ROOT_BINARY_PATH) ./cmd/runtime-cli/main.go

test: deps
	@echo "Running tests..."
	@$(GOTEST) -v ./...

clean:
	@echo "Cleaning up..."
	@$(GOCLEAN)
	rm -f $(BINARY_PATH)
	rm -f *.exe *.exe~ *.dll *.so *.dylib
	rm -f *.test
	rm -f *.out
	rm -f *.zip *.tar.gz *.tar.xz *.tar.bz2 *.tar.lzma *.tar.lz *.tar.lzo *.tgz
	rm -f cdprun-demo-linux-amd64 cdprun-demo-windows-amd64.exe cdprun-demo-darwin-amd64
	rm -rf bin/ dist/

lint:
	golangci-lint run

# Install dependencies
deps:
	go mod tidy

# Run tests with coverage
coverage:
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -func=coverage.out | grep total:

coverage-report: test
	@echo "Coverage per package:"
	@for pkg in $$(go list ./...); do \
	  go test -cover -coverprofile=profile.out $$pkg > /dev/null; \
	  if [ -f profile.out ]; then \
	    coverage=$$(go tool cover -func=profile.out | grep total: | awk '{print $$3}'); \
	    echo "$$pkg: $$coverage"; \
	    rm profile.out; \
	  fi; \
	done
	@echo "\nOverall coverage:" 
	@go test -coverprofile=coverage-all.out ./... > /dev/null
	@go tool cover -func=coverage-all.out | grep total:
	@rm -f coverage-all.out

# Format code
fmt:
	gofmt -s -w .

# Organize imports
imports:
	@echo "Organizing imports with goimports..."
	@goimports -w .

# Run security check
sec:
	gosec ./...

# Build for multiple platforms
build-all:
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o cdprun-demo-linux-amd64 -v
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o cdprun-demo-windows-amd64.exe -v
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -o cdprun-demo-darwin-amd64 -v

run: build
	@echo "Running interactive demo..."
	@./$(BINARY_PATH)

run-auto: build
	@echo "Running automated demo..."
	@./$(BINARY_PATH) --auto

.DEFAULT_GOAL := build

.PHONY: all build build-only test clean lint deps coverage coverage-report fmt imports sec build-all run run-auto