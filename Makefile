.PHONY: build test clean run run-local run-auto run-download run-package-build run-package-test run-package-promote

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=cdprun-host
BINARY_PATH=bin/$(BINARY_NAME)
# Binary used by Docker packaging scripts (must be Linux).
ROOT_BINARY_NAME=cdprun
ROOT_BINARY_PATH=bin/$(ROOT_BINARY_NAME)

all: test build

build: test
	@echo "Building host binary ($(BINARY_NAME))..."
	@mkdir -p bin
	@$(GOBUILD) -o $(BINARY_PATH) ./cmd/runtime-cli/main.go
	@echo "Building Linux binary ($(ROOT_BINARY_NAME)) for containers..."
	@GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(ROOT_BINARY_PATH) ./cmd/runtime-cli/main.go

build-only:
	@echo "Building host binary ($(BINARY_NAME)) (skipping tests)..."
	@mkdir -p bin
	@$(GOBUILD) -o $(BINARY_PATH) ./cmd/runtime-cli/main.go
	@echo "Building Linux binary ($(ROOT_BINARY_NAME)) for containers..."
	@GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(ROOT_BINARY_PATH) ./cmd/runtime-cli/main.go

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

run-download: build-only
	@echo "Running download stage..."
	@mkdir -p $(ARTIFACTS_DIR) $(DOWNLOADS_DIR) $(PACKAGES_DIR) bin
	@./$(BINARY_PATH) --config $(RUNTIME_CONFIG) --log-level error download --output json --output-dir $(DOWNLOADS_DIR) > $(ARTIFACTS_DIR)/download-summary.json

run-package-build: build-only
	@echo "Running package build stage..."
	@test -f "$(PACKAGE_MANIFEST_RESOLVED)" || (echo "missing resolved manifest: $(PACKAGE_MANIFEST_RESOLVED)" && exit 1)
	@mkdir -p $(ARTIFACTS_DIR) $(PACKAGES_DIR)
	@./$(BINARY_PATH) --config $(RUNTIME_CONFIG) --log-level error package execute \
		--stage build \
		--manifest $(PACKAGE_MANIFEST_RESOLVED) \
		--build-results $(PACKAGE_BUILD_RESULTS) \
		--test-results $(PACKAGE_TEST_RESULTS) \
		--built-manifest $(PACKAGE_MANIFEST_BUILT) \
		--tested-manifest $(PACKAGE_MANIFEST_TESTED) \
		--output json > $(PACKAGE_EXECUTE_BUILD_SUMMARY)

run-package-test: build-only
	@echo "Running package test stage..."
	@test -f "$(PACKAGE_MANIFEST_BUILT)" || (echo "missing built manifest: $(PACKAGE_MANIFEST_BUILT)" && exit 1)
	@test -f "$(PACKAGE_BUILD_RESULTS)" || (echo "missing build results: $(PACKAGE_BUILD_RESULTS)" && exit 1)
	@mkdir -p $(ARTIFACTS_DIR)
	@./$(BINARY_PATH) --config $(RUNTIME_CONFIG) --log-level error package execute \
		--stage test \
		--manifest $(PACKAGE_MANIFEST_RESOLVED) \
		--build-results $(PACKAGE_BUILD_RESULTS) \
		--test-results $(PACKAGE_TEST_RESULTS) \
		--built-manifest $(PACKAGE_MANIFEST_BUILT) \
		--tested-manifest $(PACKAGE_MANIFEST_TESTED) \
		--output json > $(PACKAGE_EXECUTE_TEST_SUMMARY)

run-package-promote: build-only
	@echo "Running package promotion stage..."
	@test -f "$(PACKAGE_MANIFEST_TESTED)" || (echo "missing tested manifest: $(PACKAGE_MANIFEST_TESTED)" && exit 1)
	@test -f "$(PACKAGE_TEST_RESULTS)" || (echo "missing test results: $(PACKAGE_TEST_RESULTS)" && exit 1)
	@mkdir -p $(ARTIFACTS_DIR)
	@./$(BINARY_PATH) --config $(RUNTIME_CONFIG) --log-level error package promote \
		--db ./downloads.db \
		--manifest $(PACKAGE_MANIFEST_TESTED) \
		--test-results $(PACKAGE_TEST_RESULTS) \
		--output json > $(PACKAGE_PROMOTE_SUMMARY)

run: run-download run-package-build run-package-test run-package-promote
	@echo "Run completed: download -> build -> test -> promote"

run-local: build
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
NODEJS_VERSION ?= 22.22.0

DOCKER_PLATFORM ?= linux/amd64

ARTIFACTS_DIR ?= ./artifacts
DOWNLOADS_DIR ?= ./downloads
PACKAGES_DIR ?= ./packages
RUNTIME_CONFIG ?= ./runtime-registry.yaml

PACKAGE_MANIFEST_RESOLVED ?= $(DOWNLOADS_DIR)/package-manifest.resolved.json
PACKAGE_MANIFEST_BUILT ?= $(ARTIFACTS_DIR)/package-manifest.built.json
PACKAGE_MANIFEST_TESTED ?= $(ARTIFACTS_DIR)/package-manifest.tested.json
PACKAGE_BUILD_RESULTS ?= $(ARTIFACTS_DIR)/package-build-results.json
PACKAGE_TEST_RESULTS ?= $(ARTIFACTS_DIR)/package-test-results.json
PACKAGE_EXECUTE_BUILD_SUMMARY ?= $(ARTIFACTS_DIR)/package-execute-build-summary.json
PACKAGE_EXECUTE_TEST_SUMMARY ?= $(ARTIFACTS_DIR)/package-execute-test-summary.json
PACKAGE_PROMOTE_SUMMARY ?= $(ARTIFACTS_DIR)/package-promote-summary.json

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

# =============================================================================
# Node.js Packaging Tests (Local Development)
# Uses cdprun download + cdprun package + fresh install smoke tests.
# NOTE: On Apple Silicon, set DOCKER_PLATFORM=linux/amd64 (default) for parity with CI.
# =============================================================================

nodejs-rpm-build:
	@echo "Building Node.js $(NODEJS_VERSION) RPM (Docker: AL2023, $(DOCKER_PLATFORM))..."
	@docker run --rm --platform=$(DOCKER_PLATFORM) -v $(PWD):/workspace -w /workspace amazonlinux:2023 /bin/bash -lc '\
		set -euo pipefail && \
		yum install -y --allowerasing ca-certificates git tar gzip findutils coreutils rpm-build rpmdevtools golang && yum clean all && \
		go mod download && \
		mkdir -p bin downloads packages && \
		go build -o ./bin/cdprun ./cmd/runtime-cli/main.go && \
		./bin/cdprun --log-level info download \
			--runtime nodejs --version "$(NODEJS_VERSION)" --exact \
			--platform linux-x64 --output json --output-dir ./downloads && \
		./bin/cdprun --log-level info package rpm \
			--runtime nodejs --version "$(NODEJS_VERSION)" \
			--db ./downloads.db --input-platform linux --input-arch x64 \
			--out-dir ./packages'

nodejs-rpm-test: nodejs-rpm-build
	@echo "Fresh-install testing Node.js $(NODEJS_VERSION) RPM (Docker: AL2023, $(DOCKER_PLATFORM))..."
	@docker run --rm --platform=$(DOCKER_PLATFORM) -v $(PWD):/workspace -w /workspace amazonlinux:2023 /bin/bash -lc '\
		set -euo pipefail && \
		yum install -y ca-certificates bash && yum clean all && \
		rpm -ivh ./packages/*.rpm && \
		bash rpm/test-nodejs.sh "/export/apps/citools/OSPO-nodejs/$(NODEJS_VERSION)" "$(NODEJS_VERSION)"'

nodejs-test: nodejs-rpm-test
	@echo "Node.js RPM tests complete."

.DEFAULT_GOAL := build

.PHONY: all build build-only test clean lint deps coverage coverage-report fmt imports sec build-all run run-local run-auto run-download run-package-build run-package-test run-package-promote nexus-download nexus-download-python nexus-download-nodejs nexus-download-dry-run python-amazonlinux python-alpine python-amazonlinux-shell python-alpine-shell nodejs-rpm-build nodejs-rpm-test nodejs-test
