#!/bin/bash
# Pre-commit hook for Dendrite
# Runs linting and tests on changed files before committing
#
# Install: make pre-commit-install
# Skip temporarily: git commit --no-verify

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔍 Running pre-commit checks...${NC}"
echo ""

# Function to print colored status
print_status() {
    if [ $1 -eq 0 ]; then
        echo -e "${GREEN}✅ $2${NC}"
    else
        echo -e "${RED}❌ $2${NC}"
        return 1
    fi
}

# Check if golangci-lint is installed
if ! command -v golangci-lint &> /dev/null; then
    echo -e "${YELLOW}⚠️  golangci-lint not installed, skipping linting${NC}"
    echo -e "${YELLOW}   Install: https://golangci-lint.run/usage/install/${NC}"
    SKIP_LINT=1
fi

# 1. Run linter on changed files
if [ -z "$SKIP_LINT" ]; then
    echo -e "${BLUE}→ Running linter...${NC}"

    # Get list of changed Go files (excluding _test.go)
    CHANGED_GO_FILES=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' | grep -v '_test.go$' || true)

    if [ -n "$CHANGED_GO_FILES" ]; then
        # Run golangci-lint on changed files only (faster)
        if echo "$CHANGED_GO_FILES" | xargs golangci-lint run --timeout=2m --new-from-rev=HEAD 2>&1; then
            print_status 0 "Linting passed"
        else
            print_status 1 "Linting failed"
            echo ""
            echo -e "${YELLOW}💡 Tip: Fix linting errors or use 'git commit --no-verify' to skip${NC}"
            exit 1
        fi
    else
        echo -e "${YELLOW}→ No Go files changed, skipping linting${NC}"
    fi
    echo ""
fi

# 2. Run tests for changed packages
echo -e "${BLUE}→ Running tests for changed packages...${NC}"

# Get list of all changed Go files (including test files)
ALL_CHANGED_GO_FILES=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' || true)

if [ -n "$ALL_CHANGED_GO_FILES" ]; then
    # Extract unique package paths from changed files
    PACKAGES=$(echo "$ALL_CHANGED_GO_FILES" | xargs -n1 dirname | sort -u | sed 's|^|./|' | paste -sd ' ')

    if [ -n "$PACKAGES" ]; then
        echo -e "${YELLOW}   Testing packages: $PACKAGES${NC}"

        # Run tests with short timeout (fast tests only)
        if go test -short -timeout=2m $PACKAGES 2>&1; then
            print_status 0 "Tests passed"
        else
            print_status 1 "Tests failed"
            echo ""
            echo -e "${YELLOW}💡 Tip: Run 'make test' to see full details${NC}"
            echo -e "${YELLOW}   Or use 'git commit --no-verify' to skip tests${NC}"
            exit 1
        fi
    fi
else
    echo -e "${YELLOW}→ No Go files changed, skipping tests${NC}"
fi

echo ""
echo -e "${GREEN}✅ All pre-commit checks passed!${NC}"
echo ""
