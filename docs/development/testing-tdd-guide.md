---
title: Test-Driven Development & Coverage Guide
parent: Development
nav_order: 4
permalink: /development/testing-tdd
---

# Test-Driven Development & Coverage Guide

**Status:** Active (January 2025)
**Coverage Goal:** 80% minimum → 90% target → 100% for new code
**Methodology:** Test-Driven Development (TDD)

## Table of Contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

## Overview

Dendrite follows **Test-Driven Development (TDD)** practices with strict coverage requirements to ensure production-ready code quality. This guide explains our testing philosophy, standards, and workflows.

### Why TDD?

- **Fewer Bugs:** Tests catch issues before production
- **Better Design:** Test-first leads to better architecture
- **Confident Refactoring:** Change code fearlessly with test safety net
- **Living Documentation:** Tests serve as executable specifications
- **Faster Development:** Less debugging time in the long run

### Coverage Philosophy

- **80% minimum:** Hard requirement enforced by CI
- **90% target:** Expected for critical packages
- **95-100% stretch:** Achievable with TDD discipline
- **100% for new code:** All patches require full test coverage

---

## Quick Start

### Running Tests Locally

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Check coverage meets threshold (80%)
make coverage-check

# Generate HTML coverage report
make coverage-report

# Run short tests (for pre-commit)
make test-short

# Run tests for specific package
go test -v ./roomserver/auth/...

# Run specific test
go test -v ./roomserver/auth/ -run TestIsServerAllowed
```

### Installing Pre-Commit Hook

```bash
# Install git hook for automatic testing
make pre-commit-install

# Hook will run on every commit:
# - Linting on changed files
# - Tests on changed packages
# - Fast feedback (<2min)

# Skip hook temporarily if needed
git commit --no-verify
```

---

## TDD Workflow

### The Red-Green-Refactor Cycle

```
┌─────────────────────────────────────────┐
│  1. RED: Write failing test first      │
│     ↓                                   │
│  2. GREEN: Write minimal code to pass  │
│     ↓                                   │
│  3. REFACTOR: Clean up, keep tests green│
│     ↓                                   │
│  (repeat)                               │
└─────────────────────────────────────────┘
```

### Example TDD Session

#### Step 1: RED - Write Failing Test

```go
// roomserver/auth/validation_test.go
package auth

import "testing"

func TestValidateEventID_ValidFormat_ReturnsNil(t *testing.T) {
    t.Parallel()

    eventID := "$valid_event_id:matrix.org"

    err := ValidateEventID(eventID)

    if err != nil {
        t.Errorf("ValidateEventID() unexpected error: %v", err)
    }
}

func TestValidateEventID_EmptyString_ReturnsError(t *testing.T) {
    t.Parallel()

    eventID := ""

    err := ValidateEventID(eventID)

    if err == nil {
        t.Error("ValidateEventID() expected error for empty string")
    }
}
```

Run test to verify it fails:
```bash
$ go test -v ./roomserver/auth/ -run TestValidateEventID
# FAIL: undefined: ValidateEventID
# This is GOOD - we want it to fail for the right reason
```

#### Step 2: GREEN - Implement Minimal Code

```go
// roomserver/auth/validation.go
package auth

import "fmt"

func ValidateEventID(eventID string) error {
    if eventID == "" {
        return fmt.Errorf("event ID cannot be empty")
    }
    // TODO: Add more validation
    return nil
}
```

Run test again:
```bash
$ go test -v ./roomserver/auth/ -run TestValidateEventID
# PASS: TestValidateEventID_ValidFormat_ReturnsNil
# PASS: TestValidateEventID_EmptyString_ReturnsError
```

#### Step 3: REFACTOR - Improve While Tests Pass

Add more test cases and improve implementation:

```go
// Add more tests
func TestValidateEventID_InvalidFormat_ReturnsError(t *testing.T) {
    tests := []struct {
        name    string
        eventID string
    }{
        {"no prefix", "invalid"},
        {"no domain", "$event_id"},
        {"invalid chars", "$event id:matrix.org"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateEventID(tt.eventID)
            if err == nil {
                t.Errorf("expected error for: %s", tt.eventID)
            }
        })
    }
}
```

Improve implementation:
```go
func ValidateEventID(eventID string) error {
    if eventID == "" {
        return fmt.Errorf("event ID cannot be empty")
    }

    if !strings.HasPrefix(eventID, "$") {
        return fmt.Errorf("event ID must start with $")
    }

    parts := strings.Split(eventID, ":")
    if len(parts) != 2 {
        return fmt.Errorf("event ID must contain domain")
    }

    // More validation...
    return nil
}
```

### Commit Convention for TDD

```bash
# Good TDD commit sequence:
git commit -m "test: Add validation tests for event IDs (RED)"
git commit -m "feat: Implement event ID validation (GREEN)"
git commit -m "refactor: Extract validation logic to helpers (REFACTOR)"

# Each commit shows TDD phase clearly
```

---

## Coverage Requirements

### Overall Project

| Metric | Current | Target | Enforcement |
|--------|---------|--------|-------------|
| Overall Coverage | ~64% | 80%+ | CI blocks PRs |
| Patch Coverage | N/A | 100% | CI blocks PRs |
| Coverage Decrease | N/A | 0% allowed | CI blocks PRs |

### Per-Package Targets

#### Tier 1: Critical Packages (95%+ required)

These packages are core to Dendrite's functionality:

- `roomserver/**` - Room state management (95%)
- `federationapi/**` - Federation protocol (95%)
- `clientapi/routing/**` - Client API endpoints (95%)

#### Tier 2: Important Packages (90%+ required)

- `syncapi/**` - Sync protocol (90%)
- `userapi/**` - User management (90%)

#### Tier 3: Standard Packages (85%+ required)

- `internal/**` - Internal utilities (85%)
- `mediaapi/**` - Media handling (80%)

#### Tier 4: Acceptable Lower Coverage

- `cmd/**` - Excluded (orchestration code, tested via integration)
- `setup/mscs/**` - Excluded (experimental features)
- `test/**` - Excluded (test utilities themselves)

### How Coverage is Enforced

**Via Codecov (`.github/codecov.yaml`):**
- Overall project: 80% minimum
- New code (patches): 100% required
- Per-package: Tier-specific targets
- Threshold: 0% (no decrease allowed)

**Via CI/CD:**
- Unit tests run on every PR
- Coverage report posted as PR comment
- PR blocked if coverage requirements not met
- Integration tests run nightly (Sytest, Complement)

**Via Pre-Commit Hook:**
- Tests run on changed packages before commit
- Fast feedback loop (<2min)
- Can skip with `--no-verify` if urgent

---

## Writing Good Tests

### Test Organization

```
package/
├── service.go              # Implementation
├── service_test.go         # Unit tests (same package)
├── service_bench_test.go   # Benchmarks (optional)
├── export_test.go          # Expose internals for testing
└── testdata/               # Test fixtures
    ├── valid_event.json
    └── invalid_event.json
```

### Test Naming Convention

```go
// Pattern: Test<FunctionName>_<Scenario>_<ExpectedBehavior>

func TestValidateUser_EmptyUsername_ReturnsError(t *testing.T)
func TestValidateUser_ValidInput_ReturnsNil(t *testing.T)
func TestProcessEvent_DuplicateEvent_Idempotent(t *testing.T)
func TestCalculateScore_MultipleItems_ReturnSum(t *testing.T)
```

### Table-Driven Tests (Preferred Pattern)

```go
func TestValidation(t *testing.T) {
    t.Parallel() // Always parallel unless testing global state

    tests := []struct {
        name    string
        input   Input
        want    Output
        wantErr bool
    }{
        {
            name:    "valid input",
            input:   Input{Value: "valid"},
            want:    Output{Result: "processed"},
            wantErr: false,
        },
        {
            name:    "empty input",
            input:   Input{Value: ""},
            wantErr: true,
        },
        {
            name:    "special chars",
            input:   Input{Value: "test@123"},
            want:    Output{Result: "test_123"},
            wantErr: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel() // Parallel subtests

            got, err := Process(tt.input)

            if (err != nil) != tt.wantErr {
                t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
                return
            }

            if !tt.wantErr && got != tt.want {
                t.Errorf("Process() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Database Tests

Use `test.WithAllDatabases()` to test both SQLite and PostgreSQL:

```go
func TestUserStorage(t *testing.T) {
    test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
        // Setup
        cfg, processCtx, close := testrig.CreateConfig(t, dbType)
        defer close()

        db, err := storage.NewDatabase(cfg)
        if err != nil {
            t.Fatalf("NewDatabase: %v", err)
        }

        // Test
        err = db.StoreUser(user)
        if err != nil {
            t.Errorf("StoreUser: %v", err)
        }

        // Verify
        retrieved, err := db.GetUser(userID)
        if err != nil {
            t.Errorf("GetUser: %v", err)
        }

        if !reflect.DeepEqual(retrieved, user) {
            t.Errorf("got %+v, want %+v", retrieved, user)
        }
    })
}
```

### HTTP Handler Tests

```go
func TestLoginHandler(t *testing.T) {
    // Setup
    cfg, _, close := testrig.CreateConfig(t, test.DBTypePostgres)
    defer close()

    req := test.NewRequest(t, "POST", "/_matrix/client/v3/login", map[string]interface{}{
        "type":     "m.login.password",
        "user":     "alice",
        "password": "secret",
    })

    rec := httptest.NewRecorder()

    // Execute
    Login(rec, req, cfg)

    // Assert
    if rec.Code != http.StatusOK {
        t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
    }

    var response LoginResponse
    if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
        t.Fatalf("decode response: %v", err)
    }

    if response.AccessToken == "" {
        t.Error("expected access_token in response")
    }
}
```

### Test Coverage Checklist

Every function should have tests for:

- [ ] **Happy path** - Expected input/output
- [ ] **Edge cases** - Empty, nil, zero, max values
- [ ] **Error cases** - Invalid input, failures
- [ ] **Boundary conditions** - Off-by-one, limits
- [ ] **Concurrent access** - Race conditions (if applicable)
- [ ] **Integration points** - External dependencies

### What NOT to Test

- Private helper functions (test through public API)
- Generated code (`*_gen.go`)
- Third-party libraries
- Trivial getters/setters (unless they have logic)

---

## Testing Utilities

### Available Test Helpers

```go
import (
    "github.com/element-hq/dendrite/test"
    "github.com/element-hq/dendrite/test/testrig"
)

// Create test configuration
cfg, processCtx, close := testrig.CreateConfig(t, test.DBTypePostgres)
defer close()

// Create test user
user := test.NewUser(t)
adminUser := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))

// Create test room
room := test.NewRoom(t, user)

// Create HTTP request
req := test.NewRequest(t, "POST", "/endpoint", body)

// Test against both databases
test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
    // Your test here
})
```

### Mock Dependencies

When you need to mock external dependencies:

```go
// Use interfaces for mockability
type UserStore interface {
    GetUser(id string) (*User, error)
    StoreUser(user *User) error
}

// Create mock in test file
type mockUserStore struct {
    users map[string]*User
}

func (m *mockUserStore) GetUser(id string) (*User, error) {
    user, ok := m.users[id]
    if !ok {
        return nil, fmt.Errorf("user not found")
    }
    return user, nil
}

// Use in test
func TestService(t *testing.T) {
    store := &mockUserStore{
        users: map[string]*User{
            "alice": {ID: "alice", Name: "Alice"},
        },
    }

    service := NewService(store)
    // Test using mock
}
```

---

## Coverage Analysis

### Viewing Coverage Reports

```bash
# Generate coverage for specific package
go test -coverprofile=coverage.out ./roomserver/auth/

# View summary
go tool cover -func=coverage.out

# Generate HTML report
go tool cover -html=coverage.out -o coverage.html

# Open in browser (macOS)
open coverage.html

# Open in browser (Linux)
xdg-open coverage.html
```

### Interpreting Coverage Reports

```
github.com/element-hq/dendrite/roomserver/auth/auth.go:45:    IsServerAllowed     95.2%
github.com/element-hq/dendrite/roomserver/auth/auth.go:78:    checkAllowed        100.0%
github.com/element-hq/dendrite/roomserver/auth/auth.go:102:   validateEvent       87.5%
total:                                                         (statements)        92.1%
```

- **Green (100%):** Fully covered
- **Yellow (50-99%):** Partially covered, add tests for missing branches
- **Red (<50%):** Poorly covered, priority for improvement

### Finding Uncovered Code

```bash
# Show only uncovered functions
make coverage-missing

# Or manually:
go tool cover -func=coverage.out | grep -v "100.0%"
```

---

## Continuous Integration

### PR Coverage Checks

When you open a PR, GitHub Actions will:

1. **Run unit tests** with coverage
2. **Upload coverage** to Codecov
3. **Post coverage report** as PR comment
4. **Block merge** if:
   - Overall coverage < 80%
   - Patch coverage < 100%
   - Coverage decreased

### Example PR Comment

```
## Coverage Report

| Coverage | Value |
|----------|-------|
| Overall  | 81.2% ✅ |
| Patch    | 100% ✅ |
| Change   | +0.8% ✅ |

### Package Coverage
- roomserver/auth: 95.2% ✅ (target: 95%)
- clientapi/routing: 92.1% ⚠️ (target: 95%)

### Files Changed
| File | Coverage | Change |
|------|----------|--------|
| auth.go | 95.2% | +2.1% ✅ |
| validation.go | 100% | new ✅ |
```

### Nightly Integration Tests

Integration tests run nightly:
- **Sytest** - Matrix compliance (796 passing tests)
- **Complement** - Federation testing
- Coverage from these is tracked separately

---

## Roadmap: 64% → 90%+ Coverage

### Phase 1: Quick Wins (Weeks 1-2) → 70%

**Target Packages:**
- `appservice/` - Add handler tests
- `mediaapi/routing/` - Add thumbnail tests
- `internal/caching/` - Add strategy tests

**Expected:** +6% coverage

### Phase 2: Business Logic (Weeks 3-4) → 75%

**Target Packages:**
- `roomserver/internal/input/` - Event validation
- `federationapi/routing/` - Federation endpoints
- `clientapi/routing/` - API handlers

**Expected:** +5% coverage

### Phase 3: Complex Scenarios (Weeks 5-6) → 80%

**Target Packages:**
- `syncapi/sync/` - Sync state machine
- `roomserver/state/` - State resolution
- `federationapi/internal/` - Federation client

**Expected:** +5% coverage

### Phase 4: Excellence (Weeks 7-10) → 85%+

**Focus Areas:**
- Edge cases and error paths
- Integration test coverage
- Concurrent access patterns
- Performance regression tests

**Expected:** +5-10% coverage

### Phase 5: Perfection (Ongoing) → 90%+

**Practices:**
- 100% coverage for all new code (enforced)
- Opportunistic legacy code coverage improvements
- Regular coverage reviews
- Continuous improvement culture

---

## Best Practices

### Do's ✅

- **Write tests first** (TDD red-green-refactor)
- **Use table-driven tests** for multiple scenarios
- **Test error cases** not just happy path
- **Use `t.Parallel()`** for independent tests
- **Use descriptive test names** that explain the scenario
- **Keep tests simple** and focused
- **Use test helpers** from `test` package
- **Run tests locally** before pushing
- **Check coverage** for your changes

### Don'ts ❌

- **Don't skip writing tests** ("I'll add them later")
- **Don't test implementation details** (test behavior)
- **Don't use `time.Sleep()`** in tests (flaky)
- **Don't test third-party code** (trust their tests)
- **Don't commit without running tests** (use pre-commit hook)
- **Don't decrease coverage** (maintain or improve)
- **Don't write tests just for coverage** (write meaningful tests)

### Common Pitfalls

**Flaky Tests:**
```go
// ❌ BAD: Time-dependent test
func TestAsync(t *testing.T) {
    go doWork()
    time.Sleep(100 * time.Millisecond) // Flaky!
    // assert result
}

// ✅ GOOD: Synchronization
func TestAsync(t *testing.T) {
    done := make(chan struct{})
    go func() {
        doWork()
        close(done)
    }()

    select {
    case <-done:
        // assert result
    case <-time.After(5 * time.Second):
        t.Fatal("timeout waiting for work")
    }
}
```

**Testing Implementation Instead of Behavior:**
```go
// ❌ BAD: Testing internal structure
func TestUserStore(t *testing.T) {
    store := NewUserStore()
    if len(store.users) != 0 {
        t.Error("internal map should be empty")
    }
}

// ✅ GOOD: Testing behavior
func TestUserStore_Empty_ReturnsError(t *testing.T) {
    store := NewUserStore()
    _, err := store.GetUser("alice")
    if err == nil {
        t.Error("expected error for non-existent user")
    }
}
```

---

## Resources

### Documentation
- [Go Testing Documentation](https://pkg.go.dev/testing)
- [Coverage Guide](coverage.md) - Detailed coverage setup
- [Contributing Guide](CONTRIBUTING.md) - General contribution guidelines

### Tools
- [golangci-lint](https://golangci-lint.run/) - Linting
- [Codecov](https://codecov.io/gh/element-hq/dendrite) - Coverage tracking
- [gotestfmt](https://github.com/gotesttools/gotestfmt) - Pretty test output

### Books & Articles
- "Test-Driven Development: By Example" - Kent Beck
- "Working Effectively with Legacy Code" - Michael Feathers
- [Go Testing Best Practices](https://go.dev/doc/tutorial/add-a-test)

---

## Getting Help

### Common Questions

**Q: My test is failing in CI but passes locally. Why?**

A: Common causes:
- Race conditions (run with `-race` flag)
- Different database setup (test both SQLite and PostgreSQL)
- Timing dependencies (avoid `time.Sleep`, use proper synchronization)
- File system differences (use `t.TempDir()` for temp files)

**Q: How do I test code that depends on external services?**

A: Use interfaces and mocks:
1. Define interface for the dependency
2. Create mock implementation in test
3. Inject dependency via constructor
4. Test using mock

**Q: Do I need 100% coverage for old code?**

A: No, but:
- Don't decrease existing coverage
- When you modify old code, add tests (TDD)
- Opportunistically improve coverage when you can
- Focus on critical paths first

**Q: Can I skip tests temporarily?**

A: Use `t.Skip()` sparingly:
```go
func TestFeature(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping in short mode")
    }
    // Long-running test
}
```

### Support Channels

- **Matrix Room:** [#dendrite-dev:matrix.org](https://matrix.to/#/#dendrite-dev:matrix.org)
- **GitHub Issues:** [element-hq/dendrite/issues](https://github.com/element-hq/dendrite/issues)
- **Code Review:** Request review on your PR for testing feedback

---

## Summary

Dendrite follows **Test-Driven Development** with **80-100% coverage goals**:

1. **Write tests first** (TDD red-green-refactor)
2. **100% coverage required** for all new code
3. **80% minimum** overall project coverage
4. **95% target** for critical packages
5. **Automated enforcement** via CI/CD
6. **Fast feedback** via pre-commit hooks
7. **Visible metrics** via Codecov badges

**Remember:** Tests are not a burden—they're an investment in code quality, confidence, and maintainability. Write them first, write them well, and the code will thank you later! 🚀
