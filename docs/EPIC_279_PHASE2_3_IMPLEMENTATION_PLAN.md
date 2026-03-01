# Epic #279 — Phase 2 & 3 Implementation Plan (Execution-Ready)

Date: 2026-02-28

## Scope and intent

This document translates Epic [#279](https://github.com/subculture-collective/vod-tender/issues/279) into an implementation-ready plan for:

- **Phase 2 (P1):** [#284](https://github.com/subculture-collective/vod-tender/issues/284) chat privacy hardening
- **Phase 3 (P2):** [#285](https://github.com/subculture-collective/vod-tender/issues/285) `processOnce()` refactor and [#286](https://github.com/subculture-collective/vod-tender/issues/286) yt-dlp progress parsing hardening

Phase 1 was completed in PR #287 (merged).

## Current baseline (verified)

- Chat recording persists raw usernames/messages in `chat_messages` via `backend/chat/chat.go`.
- No dedicated admin endpoint exists for user-level chat erasure.
- Retention exists for VOD files (`backend/vod/retention.go`), but not for chat messages.
- `processOnce()` in `backend/vod/processing.go` is large and multi-concern.
- Progress parsing currently relies on yt-dlp stderr regex in `backend/vod/vod.go`.

---

## Delivery strategy

Implement in **four focused PRs** to reduce risk and make review easier.

### PR-1: Phase 2 foundation — schema + privacy worker

**Issues:** #284  
**Goal:** Add chat retention/anonymization mechanics with safe defaults.

#### Planned changes

1. **DB migration** (`backend/db/migrations/000005_chat_privacy_foundation.*.sql`)
   - Add `username_hash` column to `chat_messages`.
   - Add `anonymized_at` column to `chat_messages`.
   - Add index for cleanup efficiency: `(channel, abs_timestamp)`.
   - Add index for erase/anonymize targeting: `(channel, username_hash)`.

2. **Privacy worker** (`backend/chat/privacy.go`)
   - Add `StartChatPrivacyJob(ctx, db, channel)`.
   - Add cycle operations:
     - `anonymizeOldMessages(...)`
     - `deleteExpiredMessages(...)`
   - Use batched updates/deletes to avoid long locks.

3. **Main wiring** (`backend/main.go`)
   - Start chat privacy job per channel worker.

4. **Config/docs baseline**
   - Add env vars to docs and config flow:
     - `CHAT_RETENTION_DAYS` (default `0`, disabled)
     - `CHAT_RETENTION_INTERVAL` (default `6h`)
     - `CHAT_ANONYMIZE_AFTER_DAYS` (default `0`, disabled)
     - `CHAT_ANONYMIZE_SALT` (required when anonymization enabled)

#### Test plan

- `backend/chat/privacy_test.go`:
  - deletes only messages older than retention cutoff
  - anonymizes usernames older than anonymize cutoff
  - idempotency across repeated runs
  - channel scoping correctness

#### Exit criteria

- No behavior change when new env vars are unset.
- Privacy worker runs safely in background with no errors in normal path.

---

### PR-2: Phase 2 completion — admin erasure endpoint + documentation

**Issues:** #284  
**Goal:** Provide GDPR-style user erasure primitive and complete operator guidance.

#### Planned changes

1. **Admin endpoint**
   - Add route in `backend/server/server.go`:
     - `DELETE /admin/chat/user/{username}`
   - Add handler file (e.g. `backend/server/handlers_admin_chat.go`):
     - Purge by plaintext username and hash match:
       - `username = $input OR username_hash = hash($input)`
     - Require channel scoping (query param or default env channel behavior).

2. **OpenAPI contract**
   - Update `backend/api/openapi.yaml` with new admin endpoint.

3. **Docs**
   - Update `docs/CONFIG.md` with chat privacy vars and examples.
   - Update `README.md` legal/privacy notice:
     - chat archival disclosure responsibility
     - retention/anonymization configuration
   - Add operator procedure to `docs/OPERATIONS.md` or `docs/RUNBOOKS.md`.

#### Test plan

- `backend/server/*_test.go`:
  - successful purge response and deleted-row count
  - method validation (`DELETE` only)
  - auth middleware compatibility (`/admin/*`)
  - channel-scope behavior

#### Exit criteria

- Acceptance criteria from #284 met:
  - retention policy exists
  - anonymization option exists
  - privacy notice documented
  - admin can purge by username

---

### PR-3: Phase 3 item A — yt-dlp progress hardening

**Issues:** #286  
**Goal:** Make progress persistence resilient to yt-dlp output changes.

#### Planned changes

1. **Structured progress output** (`backend/vod/vod.go`)
   - Add yt-dlp flags:
     - `--newline`
     - `--progress-template` with stable machine-readable format

2. **Parser extraction**
   - Create parser helper module (e.g. `backend/vod/progress_parser.go`):
     - parse template lines
     - convert size units robustly
     - graceful fallback when parse fails

3. **Replace current inline `decUnit` closure**
   - Use standard parser function with explicit unit map:
     - `B`, `KiB`, `MiB`, `GiB`, `TiB`

4. **Fallback behavior**
   - On parse error: warn/log and continue download; do not silently stop all progress updates.

#### Test plan

- `backend/vod/progress_parser_test.go`:
  - valid structured lines
  - malformed/partial lines
  - alternate unit values
  - parse fallback path
- Update/add tests in `backend/vod/vod_test.go` as needed.

#### Exit criteria

- #286 acceptance criteria satisfied with robust tests.
- Download completion and DB progress fields remain intact.

---

### PR-4: Phase 3 item B — `processOnce()` decomposition

**Issues:** #285  
**Goal:** Reduce `processOnce()` to orchestration; preserve behavior.

#### Planned changes

Refactor `backend/vod/processing.go` into composable helpers, for example:

- `checkCircuitBreaker(...)`
- `cleanupDataDirArtifacts(...)`
- `discoverAndComputeQueue(...)`
- `checkUploadThrottling(...)`
- `selectNextVOD(...)`
- `executeDownload(...)`
- `executeUpload(...)`
- `finalizeProcessing(...)`

Optional: introduce a small `processingContext` struct for shared run-state.

#### Guardrails

- No behavior changes to:
  - circuit breaker transitions
  - retry/backoff
  - upload policy checks
  - idempotency (`youtube_url` pre-check, skip-upload behavior)

#### Test plan

- Extend `backend/vod/processing_test.go` with helper-focused tests.
- Keep existing tests green without semantic changes.
- Add characterization tests before major extraction where needed.

#### Exit criteria

- `processOnce()` orchestration-only and significantly shorter.
- Existing behavior preserved, tests passing.

---

## Dependency/order plan

1. **PR-1 (#284 foundation)**
2. **PR-2 (#284 completion)**
3. **PR-3 (#286 progress hardening)**
4. **PR-4 (#285 refactor)**

Reasoning:

- P1 privacy risk handled first.
- Progress parser hardening is lower coupling than process refactor.
- Refactor lands after parser stabilization to reduce merge conflict risk.

---

## Acceptance matrix

### #284 Chat privacy

- [ ] Configurable retention period
- [ ] Cleanup job removes expired messages
- [ ] Anonymization option exists
- [ ] Documentation/privacy notice added
- [ ] Admin purge by username available

### #286 Progress hardening

- [ ] Structured progress output used
- [ ] `decUnit` replaced by robust parser
- [ ] Graceful fallback on parse issues
- [ ] Unit tests for format variations
- [ ] DB progress persistence remains correct

### #285 Refactor

- [ ] `processOnce()` under ~100 lines (or equivalent orchestration-only target)
- [ ] Extracted helpers independently tested
- [ ] Existing tests still pass
- [ ] No behavior change
- [ ] Coverage non-decreasing (practical target)

---

## Verification gates (per PR)

Run from `backend/`:

1. `go test ./...`
2. Focused package tests for changed areas (`./chat`, `./server`, `./vod`, `./db`)
3. Migration safety checks (up/down/idempotency tests)

CI expectation:

- gitleaks / govulncheck / golangci-lint / unit tests / image scans remain green.

---

## Rollout and rollback

### Rollout

- Merge PRs in order.
- Enable privacy env vars gradually:
  - start with dry/low-impact settings (`CHAT_RETENTION_DAYS=0`, anonymization disabled)
  - then enable anonymization, then retention.

### Rollback

- If privacy job issues occur: disable by env (`CHAT_RETENTION_DAYS=0`, `CHAT_ANONYMIZE_AFTER_DAYS=0`).
- If progress parser causes regressions: revert PR-3 independently.
- If refactor causes regressions: revert PR-4 only (behavior-isolated PR).

---

## Ready-to-start checklist

- [ ] Create PR-1 branch and migration files
- [ ] Implement privacy job + tests
- [ ] Create PR-2 admin endpoint + docs
- [ ] Implement parser hardening in PR-3
- [ ] Execute process refactor in PR-4 with characterization tests
- [ ] Close #284, #286, #285 and update epic #279 progress
