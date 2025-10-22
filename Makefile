# Makefile for Dendrite - Production-Ready Testing & Development
.PHONY: help test test-unit test-integration test-coverage test-race test-short lint fmt pre-commit-install coverage-check coverage-report clean build

# Default target - show help
help:
	@echo "Dendrite Development & Testing Commands"
	@echo ""
	@echo "Testing:"
	@echo "  make test                Run all unit tests"
	@echo "  make test-unit           Run unit tests (same as 'test')"
	@echo "  make test-integration    Run integration tests with PostgreSQL"
	@echo "  make test-coverage       Run tests with coverage report"
	@echo "  make test-race           Run tests with race detector"
	@echo "  make test-short          Run short tests only (for pre-commit)"
	@echo "  make coverage-check      Check coverage meets minimum threshold (70%)"
	@echo "  make coverage-report     Generate HTML coverage report"
	@echo ""
	@echo "Code Quality:"
	@echo "  make lint                Run golangci-lint"
	@echo "  make fmt                 Format code with gofmt"
	@echo "  make pre-commit-install  Install pre-commit git hook"
	@echo ""
	@echo "Build:"
	@echo "  make build               Build all binaries"
	@echo "  make clean               Clean build artifacts and coverage files"
	@echo ""
	@echo "Coverage Requirements (enforced by Codecov):"
	@echo "  - Overall project: ≥ 70%"
	@echo "  - New code (patches): ≥ 80%"
	@echo "  - High-coverage packages (appservice, internal/caching): ≥ 80%"
	@echo "  - See .github/codecov.yaml for per-component targets"

# Run all unit tests
test: test-unit

test-unit:
	@echo "Running unit tests..."
	go test -v ./...

# Run integration tests with coverage (requires PostgreSQL)
test-integration:
	@echo "Running integration tests with coverage..."
	@echo "Note: Requires PostgreSQL. Set POSTGRES_* env vars if using remote instance."
	go test -race -json -v -coverpkg=./... -coverprofile=cover.out \
		$$(go list ./... | grep -v '/cmd/') 2>&1 | gotestfmt -hide all || true
	@if [ -f cover.out ]; then \
		echo ""; \
		echo "Coverage Summary:"; \
		go tool cover -func=cover.out | grep total; \
	fi

# Run tests with coverage (unit tests only, no PostgreSQL required)
test-coverage:
	@echo "Running tests with coverage..."
	go test -race -covermode=atomic -coverprofile=coverage.out \
		-coverpkg=./... \
		$$(go list ./... | grep -v '/cmd/')
	@echo ""
	@echo "Coverage Summary:"
	@go tool cover -func=coverage.out | grep total | \
		awk '{print "Total Coverage: " $$3}' | \
		awk -F: '{printf "\033[1;36m%s\033[0m\n", $$0}'
	@echo ""
	@echo "To view detailed HTML report: make coverage-report"

# Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	go test -race -short ./...

# Run short tests only (fast, for pre-commit hooks)
test-short:
	@echo "Running short tests..."
	go test -short -timeout=2m ./...

# Check if coverage meets minimum threshold
coverage-check: test-coverage
	@echo ""
	@echo "Checking coverage threshold..."
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	TARGET=70; \
	if [ $$(echo "$$COVERAGE < $$TARGET" | bc -l) -eq 1 ]; then \
		echo "\033[1;31m❌ Coverage $$COVERAGE% is below minimum $$TARGET%\033[0m"; \
		exit 1; \
	else \
		echo "\033[1;32m✅ Coverage $$COVERAGE% meets threshold (≥$$TARGET%)\033[0m"; \
	fi

# Generate HTML coverage report
coverage-report: test-coverage
	@echo "Generating HTML coverage report..."
	go tool cover -html=coverage.out -o coverage.html
	@echo "\033[1;32m✅ Coverage report generated: coverage.html\033[0m"
	@echo "Open it with: open coverage.html (macOS) or xdg-open coverage.html (Linux)"

# Run linter
lint:
	@echo "Running golangci-lint..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "\033[1;31m❌ golangci-lint not installed\033[0m"; \
		echo "Install: https://golangci-lint.run/usage/install/"; \
		exit 1; \
	fi
	golangci-lint run

# Format code
fmt:
	@echo "Formatting code with gofmt..."
	gofmt -s -w .
	@echo "\033[1;32m✅ Code formatted\033[0m"

# Install pre-commit hook
pre-commit-install:
	@echo "Installing pre-commit hook..."
	@echo '#!/bin/sh' > .git/hooks/pre-commit
	@echo 'make test-short' >> .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "\033[1;32m✅ Pre-commit hook installed\033[0m"
	@echo "Will run 'make test-short' before each commit"

# Build all binaries
build:
	@echo "Building all binaries..."
	go build -o bin/ ./cmd/...
	@echo "\033[1;32m✅ Build complete - binaries in bin/\033[0m"

# Clean build artifacts and coverage files
clean:
	@echo "Cleaning build artifacts and coverage files..."
	rm -rf bin/
	rm -f coverage.out coverage.html cover.out
	@echo "\033[1;32m✅ Clean complete\033[0m"
