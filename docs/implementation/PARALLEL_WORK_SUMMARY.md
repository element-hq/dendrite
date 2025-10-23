# Parallel Work Summary: Engineers A & B

**Total Duration**: 8 weeks
**Strategy**: 2 engineers working in parallel on independent tracks

---

## Week-by-Week Timeline

```
┌──────────┬─────────────────────────────────┬─────────────────────────────────┐
│ Week     │ Engineer A (Admin Track)        │ Engineer B (Features Track)     │
├──────────┼─────────────────────────────────┼─────────────────────────────────┤
│ Week 1   │ Task #1: List/Search Users      │ Task #3: Rate Limiting Config   │
│          │   Database layer + API handler  │ Task #4: Prometheus Metrics     │
│          │   Search, pagination, filtering │   5 new metrics + patterns      │
├──────────┼─────────────────────────────────┼─────────────────────────────────┤
│ Week 2   │ Task #2: Deactivate User        │ Task #5: Password Reset Flow    │
│          │   Deactivation + token revoke   │   Email tokens + SMTP           │
│          │   Leave rooms + audit log       │   Privacy-preserving            │
├──────────┼─────────────────────────────────┼─────────────────────────────────┤
│ Week 3   │ Task #6: Media Quarantine       │ Task #8: URL Previews           │
│          │   Quarantine endpoints          │   Metadata extraction           │
│          │   Database schema migration     │   **CRITICAL**: SSRF protection │
├──────────┼─────────────────────────────────┼─────────────────────────────────┤
│ Week 4   │ Task #6: Media Quarantine       │ Task #8: URL Previews           │
│          │   Block downloads + reuploads   │   Caching + rate limiting       │
│          │   4 admin endpoints             │   Security hardening            │
├──────────┼─────────────────────────────────┼─────────────────────────────────┤
│ Week 5   │ Task #7: Room History Purge     │ Task #10: Thread Notifications  │
│          │   Purge logic (redact vs del)   │   Thread relation aggregation   │
│          │   Timestamp/user/event filters  │   Notification counts in /sync  │
├──────────┼─────────────────────────────────┼─────────────────────────────────┤
│ Week 6   │ Task #7: Room History Purge     │ Task #10: Thread Notifications  │
│          │   State event protection        │   Push rules for mentions       │
│          │   GDPR compliance verified      │   Thread-specific receipts      │
├──────────┼─────────────────────────────────┼─────────────────────────────────┤
│ Week 7   │ ✅ DONE - Code reviews          │ Task #9: 3PID Email Verify      │
│          │ Help Engineer B if needed       │   Email verification tokens     │
│          │                                 │   Add/list/delete 3PIDs         │
├──────────┼─────────────────────────────────┼─────────────────────────────────┤
│ Week 8   │ ✅ DONE - Final polish          │ Task #9: 3PID Email Verify      │
│          │ Documentation updates           │   Integration with Task #5      │
│          │                                 │   Duplicate prevention          │
└──────────┴─────────────────────────────────┴─────────────────────────────────┘
```

---

## Engineer A: Admin API Track (6 weeks)

**Worktree**: `/Users/user/src/dendrite-engineer-a-admin-track`

### ✅ Prerequisites Complete
- Phase 0: Admin API v1 Router (merged to main)

### Tasks

**Week 1: Task #1 - List/Search Users** (2-4 days) - P0
- Endpoint: `GET /_dendrite/admin/v1/users`
- Database: SelectUsers, CountUsers methods
- Features: Search, pagination, filtering, sorting

**Week 2: Task #2 - Deactivate User** (2-3 days) - P0, GDPR
- Endpoint: `POST /_dendrite/admin/v1/deactivate/{userID}`
- Actions: Mark deactivated, revoke tokens, leave rooms, audit log

**Week 3-4: Task #6 - Media Quarantine** (1-2 weeks) - P1, CSAM
- 4 Endpoints: Single/room/user quarantine + unquarantine
- Database: Schema migration for quarantine columns
- Logic: Block downloads, prevent reuploads

**Week 5-6: Task #7 - Room History Purge** (1-2 weeks) - P0, GDPR
- Endpoint: `POST /_dendrite/admin/v1/purge_history/{roomID}`
- Methods: Redact vs delete
- Protection: State events cannot be deleted

---

## Engineer B: Features Track (8 weeks)

**Worktree**: `/Users/user/src/dendrite-engineer-b-features-track`

### Tasks

**Week 1: Tasks #3 + #4** (5-7 days)

#### Task #3: Rate Limiting Config (2-3 days) - P0
- Per-endpoint rate limit overrides
- IP-based exemptions (CIDR notation)
- Token bucket algorithm + burst control
- Prometheus metrics

#### Task #4: Prometheus Metrics (3-4 days) - P1
- HTTP request duration histogram
- Sync duration and lag metrics
- Federation send queue depth
- Media cache hit ratio

**Week 2: Task #5 - Password Reset** (3-5 days) - P1
- Email verification token system
- Password reset endpoints
- SMTP integration
- Privacy-preserving (no user enumeration)

**Week 3-4: Task #8 - URL Previews** (1-2 weeks) - P2
- HTML metadata extraction (Open Graph)
- **CRITICAL**: SSRF protection
- Domain allow/block lists
- Caching with TTL

**Week 5-6: Task #10 - Thread Notifications** (1-2 weeks) - P1
- Thread relation aggregation (MSC3440)
- Thread notification counts in `/sync`
- Push rules for thread mentions (MSC3952)
- Thread-specific read receipts

**Week 7-8: Task #9 - 3PID Email Verification** (1-2 weeks) - P2
- Email verification token flow
- Add/list/delete 3PID endpoints
- Database schema for 3PID associations
- Integration with Task #5

---

## Collaboration Points

### Week 3: Rate Limiting Sharing
**Engineer B → Engineer A**
- Engineer A may want to reuse rate limiting implementation
- Share patterns and documentation

### Week 5: Metrics Sharing
**Engineer B → Engineer A**
- Engineer A may want to add admin metrics
- Share metric naming conventions

### Ongoing: Code Reviews
**Both Engineers**
- Review each other's PRs
- Catch bugs and share knowledge
- Rotate review priority weekly

---

## Worktree Structure

```
/Users/user/src/
├── dendrite/                              # Main repo (read-only, on main branch)
│   └── .claude/todos/                     # Shared documentation
│       ├── ENGINEER_A_TRACK_PROMPT.md
│       ├── ENGINEER_B_TRACK_PROMPT.md
│       ├── PARALLEL_WORK_SUMMARY.md       # This file
│       └── ...
│
├── dendrite-engineer-a-admin-track/       # Engineer A worktree
│   ├── WORKTREE.md                        # Quick reference
│   └── [on branch: feature/admin-list-users]
│
└── dendrite-engineer-b-features-track/    # Engineer B worktree
    ├── WORKTREE.md                        # Quick reference
    └── [on branch: feature/rate-limiting-config]
```

---

## Git Workflow

### For Each Task

**Create Branch** (if not already in worktree):
```bash
git worktree add ../dendrite-engineer-a-task2 -b feature/admin-deactivate-user
```

**Development**:
```bash
# Morning: Sync with main
git fetch origin && git merge origin/main

# Work: TDD cycles
# - Write test (Red)
# - Implement (Green)
# - Refactor (Clean)
# - Commit

# Evening: Push
git push -u origin <branch-name>
```

**After PR Merged**:
```bash
# Switch back to main repo
cd /Users/user/src/dendrite
git pull origin main

# Remove old worktree
git worktree remove /Users/user/src/dendrite-engineer-a-task1

# Create new worktree for next task
git worktree add ../dendrite-engineer-a-task2 -b feature/admin-deactivate-user
```

---

## Success Metrics

### Velocity
- **Target**: 1-2 PRs merged per week (combined)
- **Actual**: Track in `.claude/todos/IMPLEMENTATION_PROGRESS.md`

### Quality
- **Test Coverage**: ≥80% for all new code
- **No Regressions**: All existing tests must pass
- **Security**: Zero P0/P1 vulnerabilities

### Collaboration
- **Code Reviews**: ≥3 PRs reviewed per engineer
- **Review Time**: ≥90% of PRs reviewed within 24 hours
- **Knowledge Sharing**: ≥2 pairing sessions total

---

## Quick Reference

### Engineer A Resources
- **Implementation Guide**: `.claude/todos/ENGINEER_A_TRACK_PROMPT.md`
- **Worktree Guide**: `dendrite-engineer-a-admin-track/WORKTREE.md`
- **Current Branch**: `feature/admin-list-users`

### Engineer B Resources
- **Implementation Guide**: `.claude/todos/ENGINEER_B_TRACK_PROMPT.md`
- **Schedule Details**: `.claude/todos/ENGINEER_B_IMPLEMENTATION_SCHEDULE.md`
- **Worktree Guide**: `dendrite-engineer-b-features-track/WORKTREE.md`
- **Current Branch**: `feature/rate-limiting-config`

### Shared Resources
- **Task Specs**: `.claude/todos/TOP_10_PRIORITY_TASKS.md`
- **Progress Tracking**: `.claude/todos/IMPLEMENTATION_PROGRESS.md`
- **Coordination Guide**: `.claude/todos/PARALLEL_IMPLEMENTATION_GUIDE.md`
- **Codebase Guide**: `CLAUDE.md`

---

## Getting Started

### Engineer A
```bash
cd /Users/user/src/dendrite-engineer-a-admin-track
cat WORKTREE.md
cat .claude/todos/ENGINEER_A_TRACK_PROMPT.md | less
# Start Task #1 Cycle 1
```

### Engineer B
```bash
cd /Users/user/src/dendrite-engineer-b-features-track
cat WORKTREE.md
cat .claude/todos/ENGINEER_B_TRACK_PROMPT.md | less
# Start Task #3 Cycle 1
```

---

**Status**: Ready to start parallel implementation! 🚀
