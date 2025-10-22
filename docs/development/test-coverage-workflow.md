# Test Coverage Improvement Workflow

**Purpose**: Document the correct workflow for improving test coverage using Claude Code agents

**Created**: 2025-10-21
**Last Updated**: 2025-10-21

---

## Core Principles

1. **Work Autonomously**: Don't ask for direction unless truly ambiguous. Continue with next logical step.
2. **Delegate Test Writing**: Use specialized agents (unit-test-writer) instead of writing tests manually.
3. **Straight Approval Only**: Only merge PRs after getting approval with no further comments.
4. **Document Progress**: Track progress with TodoWrite and create completion reports.

---

## Agent Delegation Guide

### When to Use unit-test-writer Agent

**Trigger**: Need to write unit tests for existing code to improve coverage

**Correct Usage**:
```
Task(subagent_type="unit-test-writer", prompt="
- Target package and current coverage
- Specific functions/files needing coverage
- Coverage target (e.g., 60%)
- Existing test patterns to follow
- Request summary of coverage improvement
")
```

**What to Provide**:
- Current coverage percentage
- Target coverage percentage
- List of uncovered functions (from `go tool cover -func`)
- Existing test file patterns
- Any constraints (skip integration tests, etc.)

**What NOT to Do**:
- ❌ Write tests manually
- ❌ Skip the agent and do it yourself
- ❌ Ask user if you should use the agent

**CRITICAL: Always Validate Agent Output**

After the unit-test-writer agent completes, you MUST validate that the generated tests actually test the production code:

1. **Read the generated test file(s)**
2. **Check each test function**:
   - Does it call the production function it claims to test?
   - Does it make assertions on the function's output/behavior?
   - Or does it only create test fixtures and assert on the fixtures?

**Red Flags** (indicates fake tests):
- Test named `TestCalculateState` but never calls `calculateState()`
- Test only creates `test.NewUser()`, `test.NewRoom()` fixtures
- Test has no assertions at all, just fixture creation
- Assertions only check fixture properties, not function behavior

**Example of a FAKE test**:
```go
func TestCalculateAndSetState_WithProvidedState(t *testing.T) {
    alice := test.NewUser(t)
    room := test.NewRoom(t, alice)
    stateEventIDs := make([]string, 0)
    for _, event := range room.Events() {
        if event.StateKey() != nil {
            stateEventIDs = append(stateEventIDs, event.EventID())
        }
    }
    require.NotEmpty(t, stateEventIDs) // ❌ Only tests fixture, never calls calculateAndSetState!
}
```

**Example of a REAL test**:
```go
func TestCalculateAndSetState_WithProvidedState(t *testing.T) {
    alice := test.NewUser(t)
    room := test.NewRoom(t, alice)

    // ✅ Actually calls the function being tested
    err := calculateAndSetState(ctx, inputter, room.Events()[0], stateEventIDs)

    // ✅ Asserts on the function's behavior
    require.NoError(t, err)
    assert.Equal(t, expectedState, room.CurrentState())
}
```

**Action if Fake Tests Detected**:
1. Remove all fake test files immediately
2. Document the issue in completion report
3. Consider if the package actually needs unit tests or integration tests

### When to Use junior-code-reviewer Agent

**Trigger**: After completing a logical chunk of code (function, class, module, feature)

**Correct Usage**:
```
Task(subagent_type="junior-code-reviewer", prompt="
Review the following code for:
- Code quality
- Potential issues
- Best practices
- Security concerns
[Provide context about what was written]
")
```

**When to Use**:
- After writing significant new code (not for agent-written code)
- Before creating a PR
- After addressing complex bugs

### When to Use pr-review-analyzer Agent

**Trigger**: Need to review PR changes before merging

**Correct Usage**:
```
Task(subagent_type="pr-review-analyzer", prompt="
Review PR #123 with multi-round analysis
Focus on:
- Regression prevention
- Code quality
- Test coverage
Create actionable fix list
")
```

**When to Use**:
- Before merging any PR
- After addressing PR comments (re-review)
- When concerned about regressions

---

## Test Coverage Improvement Workflow

### Phase 1: Analysis

1. **Run coverage analysis**:
   ```bash
   go test -coverprofile=coverage.out ./package/...
   go tool cover -func=coverage.out
   ```

2. **Identify gaps**:
   - Functions with <50% coverage
   - Critical business logic at <80%
   - Easy wins (simple functions near 100%)

3. **Categorize by difficulty**:
   - **Easy**: Unit-testable logic, no mocking needed
   - **Medium**: Requires some setup/mocking
   - **Hard**: Deep integration, extensive mocking (defer or skip)

### Phase 2: Test Writing (DELEGATE TO AGENT)

**❌ WRONG - Manual Test Writing**:
```go
// Writing tests yourself manually
func TestSomeFunction(t *testing.T) {
    // ... manually writing test code
}
```

**✅ CORRECT - Delegate to Agent**:
```
1. Invoke unit-test-writer agent with:
   - Package name
   - Current coverage: X%
   - Target coverage: Y%
   - List of uncovered functions
   - Existing test patterns

2. Agent writes comprehensive tests

3. Run tests to verify they pass

4. Check coverage improvement

5. Continue to next package
```

### Phase 3: Verification

1. **Run all tests**:
   ```bash
   go test ./...
   ```

2. **Verify coverage**:
   ```bash
   go test -coverprofile=coverage_new.out ./package/...
   go tool cover -func=coverage_new.out | grep "^total"
   ```

3. **Compare before/after**:
   - Package X: 30% → 60% (+30%)
   - Functions covered: list specific functions

### Phase 4: Documentation

1. **Update TodoWrite** with completion status

2. **Create completion report** (e.g., `PHASE2_COMPLETION_REPORT.md`):
   - Coverage improvements by package
   - Tests added (count, files)
   - Functions now covered
   - Lessons learned
   - Next steps or recommendations

3. **Update main plan** (e.g., `100_PERCENT_COVERAGE_PLAN.md`) with actual results

---

## PR and Merge Workflow

### Creating PRs

**Only create PRs when user requests**. Don't auto-commit unless explicitly told.

1. **Before creating PR**:
   - All tests passing
   - Coverage targets met
   - Code reviewed (if significant changes)

2. **Create PR with gh CLI**:
   ```bash
   gh pr create --title "..." --body "$(cat <<'EOF'
   ## Summary
   - Coverage improvements
   - Tests added

   ## Test plan
   - [ ] All tests pass
   - [ ] Coverage verified

   🤖 Generated with Claude Code
   EOF
   )"
   ```

### Review and Merge Workflow

**CRITICAL**: Only merge on straight approval

1. **After PR created**:
   - Invoke pr-review-analyzer agent
   - Agent performs multi-round review
   - Agent creates actionable fix list

2. **Address feedback**:
   - Fix all issues identified
   - Re-run tests
   - **Ask for re-review** (don't auto-merge)

3. **Re-review**:
   - Invoke pr-review-analyzer again
   - Check for straight approval

4. **Merge criteria**:
   - ✅ **Straight approval** (no further comments) → merge
   - ❌ **Any comments/suggestions** → address and re-review
   - ❌ **Skip re-review** → NEVER do this

---

## Common Mistakes to Avoid

### ❌ DON'T: Write Tests Manually
```go
// Wrong - doing the agent's job
func TestMyFunction(t *testing.T) {
    // ... manually written test
}
```
**→ Use unit-test-writer agent instead**

### ❌ DON'T: Ask for Direction Mid-Task
```
"Should I:
1. Do X?
2. Do Y?
3. Do Z?"
```
**→ Continue autonomously with next logical step**

### ❌ DON'T: Merge Without Re-Review
```bash
# Wrong - merging after fixes without re-review
git merge feature-branch
```
**→ Always ask for re-review after addressing comments**

### ❌ DON'T: Auto-Commit Changes
```bash
# Wrong - committing without user request
git commit -m "Add tests"
```
**→ Only commit when user explicitly asks**

---

## Decision Tree: When to Use Which Agent

```
Need to improve test coverage?
  ├─ Yes → Use unit-test-writer agent
  │
  ├─ Just finished writing significant code?
  │   └─ Yes → Use junior-code-reviewer agent
  │
  ├─ Ready to merge PR?
  │   └─ Yes → Use pr-review-analyzer agent
  │           └─ Got feedback?
  │               ├─ Yes → Fix, then re-review
  │               └─ No (straight approval) → Merge
  │
  └─ Simple question/exploration?
      └─ Work autonomously or use general-purpose agent
```

---

## Examples

### Example 1: Coverage Improvement Task

**User**: "Improve coverage to 100%"

**Wrong Workflow**:
1. Analyze coverage ✓
2. Write tests manually ❌
3. Ask "Should I continue to next package?" ❌

**Correct Workflow**:
1. Analyze coverage ✓
2. Invoke unit-test-writer agent ✓
3. Verify tests pass ✓
4. Move to next package autonomously ✓
5. Document completion when done ✓

### Example 2: PR Review and Merge

**User**: "Create a PR for these changes"

**Wrong Workflow**:
1. Create PR ✓
2. Merge immediately ❌

**Correct Workflow**:
1. Create PR ✓
2. Invoke pr-review-analyzer ✓
3. Address feedback ✓
4. Invoke pr-review-analyzer again (re-review) ✓
5. If straight approval → merge ✓
6. If any comments → fix and repeat step 4 ✓

---

## Summary Checklist

Before proceeding with any test coverage task, verify:

- [ ] Will I delegate test writing to unit-test-writer agent?
- [ ] Am I working autonomously without unnecessary questions?
- [ ] Will I update TodoWrite to track progress?
- [ ] Will I document completion with a report?
- [ ] If creating PR: Will I get review approval before merging?
- [ ] If feedback received: Will I ask for re-review after fixes?

---

**Remember**: Agents are specialists. Use them instead of doing their work yourself.
