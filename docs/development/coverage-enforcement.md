# Coverage Enforcement Guide

This document explains how code coverage is enforced in Dendrite's CI/CD pipeline and how to work with coverage requirements.

## Overview

Dendrite uses **Codecov** for coverage tracking and enforcement, integrated with GitHub Actions. Coverage is measured on every push and pull request, with strict requirements to prevent regression.

## Coverage Targets

### Project-Wide Targets

| Metric | Target | Threshold | Description |
|--------|--------|-----------|-------------|
| **Overall Project** | ≥70% | 0.5% decrease | Minimum baseline coverage |
| **New Code (Patches)** | ≥80% | 10% flexibility | All new/modified code in PRs |

### Per-Component Targets

Coverage requirements vary by package complexity and current state:

#### High-Coverage Packages (>80%) - MAINTAIN EXCELLENCE

| Package | Target | Threshold | Current | Status |
|---------|--------|-----------|---------|--------|
| `appservice/**` | 80% | 0% | ~84% | ✅ Excellent |
| `internal/caching/**` | 95% | 0% | 99.2% | ✅ Near-perfect |

**Rationale**: These packages already have excellent coverage. Any decrease is a regression.

#### Medium-Coverage Packages (50-80%) - GRADUAL IMPROVEMENT

| Package | Target | Threshold | Current | Status |
|---------|--------|-----------|---------|--------|
| `mediaapi/**` | 55% | 1% | ~57% | ✅ Good |
| `internal/**` | 80% | 1% | varies | 🔨 Improving |

**Rationale**: Achievable targets with small flexibility for integration-heavy code.

#### Core Packages - REALISTIC TARGETS

| Package | Target | Threshold | Current | Status |
|---------|--------|-----------|---------|--------|
| `roomserver/**` | 60% | 1% | varies | 🎯 Target |
| `syncapi/**` | 65% | 1% | varies | 🎯 Target |
| `userapi/**` | 70% | 1% | varies | 🎯 Target |
| `clientapi/**` | 50% | 1% | ~20% | 📈 Growing |
| `federationapi/**` | 40% | 1% | ~12% | 📈 Growing |

**Rationale**: Complex packages with integration testing needs. Targets set at current coverage + headroom.

### Stricter Patch Requirements

New code in high-coverage packages has stricter requirements:

| Package | Patch Target | Threshold | Rationale |
|---------|-------------|-----------|-----------|
| `appservice/**` | 90% | 5% | Already excellent, keep high standards |
| `internal/caching/**` | 95% | 5% | Near-perfect, maintain quality |
| `mediaapi/**` | 70% | 10% | Integration-heavy, moderate flexibility |

## How Coverage Enforcement Works

### 1. GitHub Actions Workflow

Coverage is collected in the `integration` job (`.github/workflows/dendrite.yml`):

```yaml
- run: go test -race -json -v -coverpkg=./... -coverprofile=cover.out $(go list ./... | grep -v '/cmd/') 2>&1 | gotestfmt -hide all
  env:
    POSTGRES_HOST: localhost
    POSTGRES_USER: postgres
    POSTGRES_PASSWORD: postgres
    POSTGRES_DB: dendrite

- name: Upload coverage to Codecov
  uses: codecov/codecov-action@v5
  with:
    flags: unittests
    fail_ci_if_error: true
    token: ${{ secrets.CODECOV_TOKEN }}
```

**Key Points**:
- Coverage is collected with `-coverprofile=cover.out`
- Tests run with race detector (`-race`)
- Excludes `/cmd/` directory (binaries, not libraries)
- Uploads to Codecov with `fail_ci_if_error: true` (CI fails if upload fails)

### 2. Codecov Configuration

All coverage rules are defined in `.github/codecov.yaml`:

```yaml
coverage:
  status:
    project:
      default:
        target: 70%           # Overall project minimum
        threshold: 0.5%       # Max decrease allowed

    patch:
      default:
        target: 80%           # New code minimum
        threshold: 10%        # Flexibility for complex code
```

### 3. PR Checks

When you open a PR, Codecov automatically:

1. **Compares coverage** to the base branch (usually `main`)
2. **Checks project coverage** against 70% target
3. **Checks patch coverage** (your new code) against 80% target
4. **Checks component coverage** for affected packages
5. **Posts a comment** with detailed coverage report
6. **Passes or fails** the CI check based on targets

## Working with Coverage Requirements

### Checking Coverage Locally

Before pushing your changes:

```bash
# Run tests with coverage
make test-coverage

# Check if coverage meets threshold
make coverage-check

# Generate HTML report for detailed view
make coverage-report
```

### Understanding Coverage Reports

#### Terminal Output

```bash
make test-coverage
```

Shows total coverage percentage in cyan:

```
Coverage Summary:
Total Coverage: 68.4%
```

#### HTML Report

```bash
make coverage-report
```

Generates `coverage.html` with:
- Green: Covered lines
- Red: Uncovered lines
- Gray: Not executable (comments, imports)

#### Codecov PR Comment

Codecov posts a comment on your PR showing:

```
Coverage: 68.40% (+0.12%)
Files: 5 changed
```

Plus a detailed breakdown by file and component.

### When Coverage Checks Fail

#### Scenario 1: Overall Project Coverage Decreased

```
❌ Project coverage decreased from 70.5% to 69.8%
```

**Solutions**:
1. **Add tests** for your new code
2. **Check if you deleted tests** accidentally
3. **If refactoring**, ensure equivalent coverage

#### Scenario 2: Patch Coverage Too Low

```
❌ Patch coverage: 45% (target: 80%)
```

**Solutions**:
1. **Write unit tests** for new functions
2. **Add integration tests** for HTTP handlers
3. **Request threshold adjustment** if code is truly untestable (with justification)

#### Scenario 3: Component Coverage Decreased

```
❌ internal/caching coverage: 98.5% → 97.8% (target: 95%, threshold: 0%)
```

**Solutions**:
1. **Add tests** for new code in that package
2. **Don't delete existing tests** without replacement
3. High-coverage packages have **zero tolerance** for decrease

### Legitimate Reasons for Coverage Decrease

Sometimes coverage decrease is justified:

1. **Removing dead code** - Coverage may drop if you remove well-tested code
2. **Refactoring with equivalent tests** - Temporary decrease during refactor
3. **Adding integration-heavy code** - Some code is better tested with Complement/Sytest

**Process**:
1. Document the reason in PR description
2. Request maintainer review
3. May require codecov.yaml adjustment (rare)

## Testing Strategy by Package

### Unit Tests (Prefer for These)

- `internal/**` - Utilities and helpers
- `appservice/**` - Application service logic
- Storage layer (`storage/tables/*_table_test.go`)
- Helper functions and validation

### Integration Tests (Better for These)

- HTTP handlers (`clientapi/routing`, `mediaapi/routing`)
- Federation flows (`federationapi/**`)
- Complex state resolution (`roomserver/state`)
- End-to-end workflows

### External Tests (Use Complement/Sytest)

- Multi-homeserver federation
- Client-server API compliance
- State resolution edge cases
- Event authorization

## Codecov Features in Use

### Flag Management

Tests are tagged with `flags: unittests` for future multi-suite support:

```yaml
flags:
  unittests:
    paths:
      - "!cmd/"
      - "!test/"
    carryforward: false
```

Future additions could include:
- `sytest` - Sytest integration coverage
- `complement` - Complement integration coverage

### Ignored Paths

These paths are excluded from coverage:

```yaml
ignore:
  - "cmd/**"                 # Binaries
  - "**/*_test.go"           # Test files
  - "**/mocks/**"            # Mocks
  - "**/*_gen.go"            # Generated code
  - "test/**"                # Test utilities
```

### GitHub Annotations

Codecov adds inline annotations on PR diffs:

```yaml
github_checks:
  annotations: true
```

Shows which lines are covered/uncovered directly in GitHub's diff view.

## Best Practices

### 1. Write Tests First (TDD)

For new features:
1. Write test with expected behavior
2. Implement feature until test passes
3. Coverage happens naturally

### 2. Test While Developing

Don't wait until PR time:

```bash
# Quick feedback loop
go test ./package/being/modified/...

# Check coverage periodically
go test -cover ./package/being/modified/...
```

### 3. Use Table-Driven Tests

Maximize coverage per test:

```go
func TestValidation(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"valid", "good@example.com", false},
        {"invalid", "bad", true},
        {"empty", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := Validate(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### 4. Focus on Valuable Tests

Don't write tests just for coverage:
- Test behavior, not implementation
- Test edge cases and error paths
- Don't test external libraries
- Skip truly untestable code (defensive panics)

### 5. Document Untestable Code

For legitimately untestable code, document why:

```go
// This panic should never happen in practice as validation occurs earlier.
// Coverage: Defensive panic, cannot be tested without breaking invariants.
if user == nil {
    panic("user should never be nil here")
}
```

## Troubleshooting

### "Coverage: unknown" on PR

**Cause**: Codecov didn't receive coverage data

**Solutions**:
1. Check GitHub Actions logs for upload errors
2. Verify `CODECOV_TOKEN` secret is set
3. Check if integration tests failed before upload

### Coverage Decreases Unexpectedly

**Cause**: Base branch coverage changed

**Solution**: Rebase your PR on latest `main`:

```bash
git fetch origin main
git rebase origin/main
```

### Codecov Check Never Completes

**Cause**: Codecov processing delay

**Solutions**:
1. Wait 5-10 minutes (normal for large PRs)
2. Check codecov.io for processing status
3. Re-run CI if stuck >30 minutes

## Updating Coverage Targets

Coverage targets in `.github/codecov.yaml` should be updated when:

1. **Package reaches new baseline** - Raise target to lock in gains
2. **Major refactor** - May need temporary threshold increase
3. **Package maturity change** - e.g., experimental → stable

**Process**:
1. Create PR updating `.github/codecov.yaml`
2. Document reasoning (link to coverage improvements)
3. Get maintainer approval

## References

- **Codecov Docs**: https://docs.codecov.com/
- **Codecov YAML Reference**: https://docs.codecov.com/docs/codecov-yaml
- **GitHub Actions Integration**: https://docs.codecov.com/docs/github-actions
- **Go Coverage Guide**: https://go.dev/blog/cover
- **Dendrite Testing Workflow**: `docs/development/test-coverage-workflow.md`
- **Dendrite Coverage Plan**: `100_PERCENT_COVERAGE_PLAN.md`

## Summary

| What | Where | Target |
|------|-------|--------|
| **Overall Coverage** | `.github/codecov.yaml` | ≥70% |
| **New Code** | `.github/codecov.yaml` | ≥80% |
| **Local Check** | `make coverage-check` | ≥70% |
| **View Report** | `make coverage-report` | HTML |
| **CI Upload** | `.github/workflows/dendrite.yml` | Auto |

**Golden Rules**:
1. ✅ Test new code thoroughly (≥80%)
2. ✅ Don't decrease high-coverage packages
3. ✅ Use integration tests for complex flows
4. ✅ Document untestable code
5. ✅ Check coverage before pushing

---

**Document Version**: 1.0
**Last Updated**: October 21, 2025
**Maintained By**: Dendrite Developers
