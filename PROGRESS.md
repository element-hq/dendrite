# Remaining Tasks - Priority Order

## Completed This Session ✅
- Task #6: Media Quarantine (90% - core features committed to main)
- Task #10: Thread Notifications (100% - committed to main)
- Task #7: Deferred to BACKLOG.md (requires schema migration decision)

## Remaining Tasks (In Priority Order)

### Task #5: Password Reset ⚡ P1
- [ ] Cycle 1: Token storage (database schema + migrations)
- [ ] Cycle 2: Request token endpoint (POST /account/password/email/requestToken)
- [ ] Cycle 3: Reset password endpoint (validate token, update password)
- [ ] Quality gate passed
- [ ] Work committed to branch
- **Effort**: S (3-5 days)
- **Priority**: P1 (High - basic user expectation)

### Task #9: 3PID Email Verification ✨ P2
- [ ] Cycle 1: Verification tokens (database schema)
- [ ] Cycle 2: Request verification endpoint
- [ ] Cycle 3: Add 3PID endpoint (bind email to account)
- [ ] Quality gate passed
- [ ] Work committed to branch
- **Effort**: M (1-2 weeks)
- **Priority**: P2 (Medium - enables registration flows)

### Task #8: URL Previews ✨ P2
- [ ] Cycle 1: SSRF protection (IP allowlist/denylist, DNS rebinding)
- [ ] Cycle 2: URL fetching (OpenGraph parsing)
- [ ] Cycle 3: Caching (24h TTL)
- [ ] Cycle 4: API endpoint (GET /media/v3/preview_url)
- [ ] Quality gate passed
- [ ] Work committed to branch
- **Effort**: M (1-2 weeks)
- **Priority**: P2 (Medium - UX enhancement)
- **Security Review**: Mandatory before merge

## Follow-Up Tasks (Backlog)

### Task #6b: Complete Media Quarantine Features
See `BACKLOG.md` for detailed specifications:
- Room-level quarantine endpoint (requires media→room event mapping)
- Unquarantine DELETE endpoint
- Validation/error test coverage improvements
- **Priority**: P2 (Enhancement to completed Task #6)

### Task #7: Room History Purge
See `BACKLOG.md` for detailed specifications:
- Requires architectural decision on schema migration
- 4 implementation options documented (A-D)
- **Priority**: P0 (GDPR Critical - deferred pending decision)

## Notes
- All tasks follow TDD methodology (write tests first)
- Target ≥80% test coverage for new code
- Always run `make test-race` before committing
- See `.claude/todos/TOP_10_PRIORITY_TASKS.md` for detailed task specifications
