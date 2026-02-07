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

# Nexus proxy download targets
nexus-download:
	@echo "Downloading all runtimes through Nexus proxy..."
	@python3 scripts/nexus_proxy_download.py

nexus-download-python:
	@echo "Downloading Python through Nexus proxy..."
	@python3 scripts/nexus_proxy_download.py --runtime python

nexus-download-nodejs:
	@echo "Downloading Node.js through Nexus proxy..."
	@python3 scripts/nexus_proxy_download.py --runtime nodejs

nexus-download-dry-run:
	@echo "Checking what would be downloaded (dry run)..."
	@python3 scripts/nexus_proxy_download.py --dry-run

# =============================================================================
# Python Package Build (Local Development)
# Build Python packages locally using Docker
# =============================================================================

PYTHON_VERSION ?= 3.13.11

# Build Python RPM for Amazon Linux 2023
python-amazonlinux:
	@echo "Building Python $(PYTHON_VERSION) RPM for Amazon Linux 2023..."
	@docker run --rm -v $(PWD):/workspace -w /workspace amazonlinux:2023 /bin/bash -c '\
		set -e && \
		yum install -y --allowerasing rpm-build rpmdevtools curl tar gzip gcc gcc-c++ make \
			openssl-devel bzip2-devel libffi-devel zlib-devel readline-devel sqlite-devel \
			ncurses-devel xz-devel tk-devel gdbm-devel libuuid-devel findutils && \
		rpmdev-setuptree && \
		curl -L --fail -o ~/rpmbuild/SOURCES/Python-$(PYTHON_VERSION).tgz \
			"https://www.python.org/ftp/python/$(PYTHON_VERSION)/Python-$(PYTHON_VERSION).tgz" && \
		cp rpm/python.spec ~/rpmbuild/SPECS/ && \
		rpmbuild -bb ~/rpmbuild/SPECS/python.spec \
			--define "runtime_version $(PYTHON_VERSION)" \
			--define "_topdir $$HOME/rpmbuild" && \
		cp ~/rpmbuild/RPMS/*/*.rpm /workspace/ && \
		echo "RPM built successfully:" && \
		ls -lh /workspace/*.rpm'

# Build Python tarball for Alpine Linux
python-alpine:
	@echo "Building Python $(PYTHON_VERSION) for Alpine Linux..."
	@docker run --rm -v $(PWD):/workspace -w /workspace alpine:3.19 /bin/sh -c '\
		set -e && \
		apk add --no-cache curl tar gzip gcc g++ make musl-dev linux-headers \
			openssl-dev bzip2-dev libffi-dev zlib-dev readline-dev sqlite-dev \
			ncurses-dev xz-dev tk-dev gdbm-dev libuuid util-linux-dev tcl-dev expat-dev && \
		curl -L --fail -o /tmp/Python-$(PYTHON_VERSION).tgz \
			"https://www.python.org/ftp/python/$(PYTHON_VERSION)/Python-$(PYTHON_VERSION).tgz" && \
		cd /tmp && tar -xzf Python-$(PYTHON_VERSION).tgz && cd Python-$(PYTHON_VERSION) && \
		./configure --prefix=/export/apps/citools/python/python-$(PYTHON_VERSION) \
			--enable-optimizations --with-lto --with-system-ffi --with-computed-gotos \
			--enable-ipv6 --enable-loadable-sqlite-extensions --with-ensurepip=upgrade && \
		make -j$$(nproc) && \
		DESTDIR=/tmp/python-install make install && \
		cd /tmp/python-install && \
		tar -czf /workspace/python-$(PYTHON_VERSION)-alpine319-x86_64.tar.gz . && \
		echo "Alpine package built successfully:" && \
		ls -lh /workspace/python-$(PYTHON_VERSION)-alpine*.tar.gz'

# Interactive shells for debugging
python-amazonlinux-shell:
	@echo "Starting interactive shell in Amazon Linux 2023 container..."
	@docker run --rm -it -v $(PWD):/workspace -w /workspace amazonlinux:2023 /bin/bash

python-alpine-shell:
	@echo "Starting interactive shell in Alpine Linux container..."
	@docker run --rm -it -v $(PWD):/workspace -w /workspace alpine:3.19 /bin/sh

.DEFAULT_GOAL := build

.PHONY: all build build-only test clean lint deps coverage coverage-report fmt imports sec build-all run run-auto nexus-download nexus-download-python nexus-download-nodejs nexus-download-dry-run python-amazonlinux python-alpine python-amazonlinux-shell python-alpine-shell