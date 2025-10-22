# Makefile for Dendrite - Production-Ready Testing & Development
.PHONY: help test test-unit test-integration test-coverage test-race test-short lint fmt pre-commit-install coverage-check coverage-report clean build

# Default target - show help
help:
\t@echo "Dendrite Development & Testing Commands"
\t@echo ""
\t@echo "Testing:"
\t@echo "  make test                Run all unit tests"
\t@echo "  make test-unit           Run unit tests (same as 'test')"
\t@echo "  make test-integration    Run integration tests with PostgreSQL"
\t@echo "  make test-coverage       Run tests with coverage report"
\t@echo "  make test-race           Run tests with race detector"
\t@echo "  make test-short          Run short tests only (for pre-commit)"
\t@echo "  make coverage-check      Check coverage meets minimum threshold (70%)"
\t@echo "  make coverage-report     Generate HTML coverage report"
\t@echo ""
\t@echo "Code Quality:"
\t@echo "  make lint                Run golangci-lint"
\t@echo "  make fmt                 Format code with gofmt"
\t@echo "  make pre-commit-install  Install pre-commit git hook"
\t@echo ""
\t@echo "Build:"
\t@echo "  make build               Build all binaries"
\t@echo "  make clean               Clean build artifacts and coverage files"
\t@echo ""
\t@echo "Coverage Requirements (enforced by Codecov):"
\t@echo "  - Overall project: ≥ 70%"
\t@echo "  - New code (patches): ≥ 80%"
\t@echo "  - High-coverage packages (appservice, internal/caching): ≥ 80%"
\t@echo "  - See .github/codecov.yaml for per-component targets"

# Run all unit tests
test: test-unit

test-unit:
\t@echo "Running unit tests..."
\tgo test -v ./...

# Run integration tests with coverage (requires PostgreSQL)
test-integration:
\t@echo "Running integration tests with coverage..."
\t@echo "Note: Requires PostgreSQL. Set POSTGRES_* env vars if using remote instance."
\tgo test -race -json -v -coverpkg=./... -coverprofile=cover.out \
\t\t$$(go list ./... | grep -v '/cmd/') 2>&1 | gotestfmt -hide all || true
\t@if [ -f cover.out ]; then \
\t\techo ""; \
\t\techo "Coverage Summary:"; \
\t\tgo tool cover -func=cover.out | grep total; \
\tfi

# Run tests with coverage (unit tests only, no PostgreSQL required)
test-coverage:
\t@echo "Running tests with coverage..."
\tgo test -race -covermode=atomic -coverprofile=coverage.out \
\t\t-coverpkg=./... \
\t\t$$(go list ./... | grep -v '/cmd/')
\t@echo ""
\t@echo "Coverage Summary:"
\t@go tool cover -func=coverage.out | grep total | \
\t\tawk '{print "Total Coverage: " $$3}' | \
\t\tawk -F: '{printf "\\033[1;36m%s\\033[0m\\n", $$0}'
\t@echo ""
\t@echo "To view detailed HTML report: make coverage-report"

# Run tests with race detector
test-race:
\t@echo "Running tests with race detector..."
\tgo test -race -short ./...

# Run short tests only (fast, for pre-commit hooks)
test-short:
\t@echo "Running short tests..."
\tgo test -short -timeout=2m ./...

# Check if coverage meets minimum threshold
coverage-check: test-coverage
\t@echo ""
\t@echo "Checking coverage threshold..."
\t@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
\tTARGET=70; \
\tif [ $$(echo "$$COVERAGE < $$TARGET" | bc -l) -eq 1 ]; then \
\t\techo "\\033[1;31m❌ Coverage $$COVERAGE% is below minimum $$TARGET%\\033[0m"; \
\t\texit 1; \
\telse \
\t\techo "\\033[1;32m✅ Coverage $$COVERAGE% meets threshold (≥$$TARGET%)\\033[0m"; \
\tfi

# Generate HTML coverage report
coverage-report: test-coverage
\t@echo "Generating HTML coverage report..."
\tgo tool cover -html=coverage.out -o coverage.html
\t@echo "\\033[1;32m✅ Coverage report generated: coverage.html\\033[0m"
\t@echo "Open it with: open coverage.html (macOS) or xdg-open coverage.html (Linux)"

# Run linter
lint:
\t@echo "Running golangci-lint..."
\t@if ! command -v golangci-lint >/dev/null 2>&1; then \
\t\techo "\\033[1;31m❌ golangci-lint not installed\\033[0m"; \
\t\techo "Install: https://golangci-lint.run/usage/install/"; \
\t\texit 1; \
\tfi
\tgolangci-lint run --timeout=5m

# Format code
fmt:
\t@echo "Formatting code..."
\tgofmt -s -w .
\t@echo "\\033[1;32m✅ Code formatted\\033[0m"

# Install pre-commit hook
pre-commit-install:
\t@if [ ! -f scripts/pre-commit.sh ]; then \
\t\techo "\\033[1;31m❌ scripts/pre-commit.sh not found\\033[0m"; \
\t\techo "Create it first with the pre-commit script from the testing guide."; \
\t\texit 1; \
\tfi
\t@mkdir -p .git/hooks
\tcp scripts/pre-commit.sh .git/hooks/pre-commit
\tchmod +x .git/hooks/pre-commit
\t@echo "\\033[1;32m✅ Pre-commit hook installed\\033[0m"
\t@echo "The hook will run linting and tests on changed files before each commit."
\t@echo "To skip the hook temporarily, use: git commit --no-verify"

# Build all binaries
build:
\t@echo "Building all binaries..."
\tgo build -trimpath -v -o "bin/" ./cmd/...
\t@echo "\\033[1;32m✅ Build complete: bin/\\033[0m"

# Clean build artifacts and coverage files
clean:
\t@echo "Cleaning build artifacts and coverage files..."
\trm -rf bin/
\trm -f coverage.out coverage.html cover.out
\trm -f *.coverprofile *.log
\t@echo "\\033[1;32m✅ Clean complete\\033[0m"

# Quick development workflow (lint + test + coverage check)
check: lint test-short coverage-check
\t@echo ""
\t@echo "\\033[1;32m✅ All checks passed!\\033[0m"
