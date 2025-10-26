# OAuth Token Envelope Encryption: Implementation Checklist

**Related Design**: [TOKEN_ENCRYPTION_ENVELOPE_DESIGN.md](./TOKEN_ENCRYPTION_ENVELOPE_DESIGN.md)  
**Status**: Approved approach pending implementation  
**Target Completion**: Q1 2026

This document provides a breakdown of implementation tasks based on the approved AWS KMS envelope encryption design.

---

## Implementation Issues to Create

### Issue #1: AWS KMS Infrastructure Setup
**Labels**: `infrastructure`, `security`, `priority:high`  
**Milestone**: Feature Completion  
**Estimated Effort**: 1 day

**Description:**
Set up AWS KMS infrastructure for OAuth token envelope encryption as specified in the design document.

**Tasks:**
- [ ] Create KMS Customer Master Key (CMK) using Terraform
  - Description: "vod-tender OAuth token encryption"
  - Enable automatic key rotation (yearly)
  - Set deletion window: 30 days
  - Tags: Application=vod-tender, Purpose=oauth-token-encryption
- [ ] Create KMS key alias: `alias/vod-tender-{environment}`
- [ ] Create IAM policy for KMS access (decrypt, encrypt, describe)
  - Restrict with encryption context: `Application=vod-tender, Purpose=oauth-token`
- [ ] Attach policy to EKS service account IAM role (IRSA)
- [ ] Enable CloudTrail logging for KMS key
- [ ] Test KMS connectivity from staging environment
- [ ] Document KMS key ARN and alias in deployment docs

**Acceptance Criteria:**
- KMS CMK created and accessible via alias
- IAM policy grants minimal required permissions
- CloudTrail logs show KMS API calls
- Terraform modules committed to infrastructure repo

**Dependencies:** None

---

### Issue #2: Crypto Package KMS Integration
**Labels**: `backend`, `security`, `priority:high`  
**Milestone**: Feature Completion  
**Estimated Effort**: 2 days

**Description:**
Implement KMS envelope encryption in the crypto package with DEK caching and comprehensive tests.

**Tasks:**
- [ ] Add AWS SDK dependency: `github.com/aws/aws-sdk-go-v2/service/kms`
- [ ] Create `backend/crypto/kms.go` with `KMSEnvelopeEncryptor` implementation
  - `Encrypt()`: Generate DEK, encrypt data with AES-256-GCM, encrypt DEK with KMS
  - `Decrypt()`: Decrypt DEK with KMS (cached), decrypt data with DEK
  - Ciphertext format: `kms:v2:<enc_dek_base64>:<data_ciphertext_base64>`
  - DEK caching with configurable TTL (default 5 minutes)
  - Encryption context in all KMS calls
- [ ] Update `backend/crypto/crypto.go` to export `EncryptString`/`DecryptString` helpers
- [ ] Create comprehensive unit tests in `backend/crypto/kms_test.go`
  - Test round-trip encryption/decryption
  - Test DEK caching (cache hits reduce KMS calls)
  - Test encryption context validation
  - Test error handling (KMS unavailable, invalid ciphertext)
  - Mock KMS client for deterministic tests
- [ ] Add integration test with real KMS (optional, gated by env var)
- [ ] Benchmark encryption/decryption performance

**Acceptance Criteria:**
- `KMSEnvelopeEncryptor` implements `Encryptor` interface
- All unit tests pass with >90% code coverage
- Benchmarks show <100ms p99 latency for encrypt/decrypt
- DEK cache reduces KMS API calls by >90% in typical usage

**Dependencies:** Issue #1 (KMS infrastructure for integration tests)

---

### Issue #3: Database Schema & Dual-Mode Support
**Labels**: `backend`, `database`, `priority:high`  
**Milestone**: Feature Completion  
**Estimated Effort**: 1 day

**Description:**
Update database schema and token storage logic to support dual-mode operation (direct AES + KMS envelope).

**Tasks:**
- [ ] Add database migration for `encryption_method` column
  - `ALTER TABLE oauth_tokens ADD COLUMN IF NOT EXISTS encryption_method TEXT DEFAULT 'direct'`
  - Values: `plaintext` (v0), `direct-aes` (v1), `kms-envelope` (v2)
- [ ] Update `db.initEncryptor()` to check `KMS_KEY_ID` environment variable
  - If `KMS_KEY_ID` set: create `KMSEnvelopeEncryptor`
  - Else if `ENCRYPTION_KEY` set: create `AESEncryptor` (legacy)
  - Else: log warning about plaintext storage
- [ ] Update `UpsertOAuthTokenForChannel()` to detect encryption method
  - Set `encryption_version=2` and `encryption_method='kms-envelope'` for KMS
  - Set `encryption_version=1` and `encryption_method='direct-aes'` for legacy
  - Store KMS key ARN in `encryption_key_id` column
- [ ] Update `GetOAuthTokenForChannel()` to handle all versions (0, 1, 2)
  - Version 0: return plaintext
  - Version 1: decrypt with `AESEncryptor` (use `ENCRYPTION_KEY`)
  - Version 2: decrypt with `KMSEnvelopeEncryptor` (use `KMS_KEY_ID`)
- [ ] Add unit tests for dual-mode read/write
  - Test reading version=1 tokens with `ENCRYPTION_KEY` set
  - Test reading version=2 tokens with `KMS_KEY_ID` set
  - Test writing new tokens in KMS mode
  - Test backward compatibility (v1 → v2 migration)

**Acceptance Criteria:**
- Database migration runs idempotently (safe to run multiple times)
- Code reads both direct-AES and KMS-envelope formats correctly
- New tokens use KMS envelope encryption when `KMS_KEY_ID` is set
- All existing unit tests still pass
- New tests cover dual-mode scenarios

**Dependencies:** Issue #2 (crypto package KMS implementation)

---

### Issue #4: Token Migration Script
**Labels**: `backend`, `migration`, `priority:medium`  
**Milestone**: Feature Completion  
**Estimated Effort**: 0.5 days

**Description:**
Create a one-time migration script to re-encrypt existing tokens from direct-AES to KMS envelope encryption.

**Tasks:**
- [ ] Create `backend/cmd/migrate-tokens/main.go` CLI tool
  - Select all tokens where `encryption_version=1`
  - Decrypt each token with `AESEncryptor` (using `ENCRYPTION_KEY`)
  - Re-encrypt with `KMSEnvelopeEncryptor` (using `KMS_KEY_ID`)
  - Update database: set `encryption_version=2`, `encryption_method='kms-envelope'`
  - Log progress (e.g., "Migrated 2/3 tokens")
  - Atomic updates (transaction per token)
  - Dry-run mode (`--dry-run` flag)
- [ ] Add validation query to check migration status
  ```sql
  SELECT encryption_version, encryption_method, COUNT(*) 
  FROM oauth_tokens 
  GROUP BY encryption_version, encryption_method;
  ```
- [ ] Document migration procedure in `docs/OPERATIONS.md`
  - Prerequisites: `KMS_KEY_ID` set, dual-mode code deployed
  - How to run: `kubectl exec -it <pod> -- /app/migrate-tokens`
  - How to verify: Run validation query, expect only version=2
  - Rollback: Re-enable `ENCRYPTION_KEY`, tokens remain readable

**Acceptance Criteria:**
- Migration script successfully re-encrypts all tokens
- Dry-run mode shows what would be changed without modifying DB
- Migration is idempotent (safe to run multiple times)
- Documentation includes step-by-step procedure

**Dependencies:** Issue #3 (dual-mode support)

---

### Issue #5: Monitoring & Alerting
**Labels**: `observability`, `sre`, `priority:medium`  
**Milestone**: Feature Completion  
**Estimated Effort**: 1 day

**Description:**
Set up monitoring and alerting for KMS envelope encryption operations.

**Tasks:**
- [ ] Add Prometheus metrics to crypto package
  - `vod_kms_encrypt_duration_seconds` (histogram)
  - `vod_kms_decrypt_duration_seconds` (histogram)
  - `vod_kms_encrypt_errors_total` (counter)
  - `vod_kms_decrypt_errors_total` (counter)
  - `vod_kms_cache_hit_rate` (gauge, 0-1)
  - `vod_kms_cache_size` (gauge, number of cached DEKs)
- [ ] Create CloudWatch alarms (Terraform)
  - `KMSDecryptErrors`: Alert if >5 errors in 5 minutes
  - `KMSHighLatency`: Alert if p99 > 500ms
  - `KMSUnauthorized`: Alert on any IAM permission denied errors
- [ ] Create Grafana dashboard for KMS metrics
  - Panel: Decrypt latency (p50, p99) over time
  - Panel: Cache hit rate over time
  - Panel: Error rate by error type
  - Panel: KMS API request count per hour
- [ ] Add application logging for key events
  - Log on startup: "KMS envelope encryption enabled" (with key ARN)
  - Log on decrypt error: Include correlation ID, error type
  - Do NOT log plaintext tokens or DEKs in logs
- [ ] Document alert response procedures in `docs/RUNBOOKS.md`
  - High decrypt error rate → Check IAM permissions, KMS key status
  - High latency → Check AWS status, consider increasing cache TTL

**Acceptance Criteria:**
- Prometheus metrics exported at `/metrics` endpoint
- CloudWatch alarms fire test alerts successfully
- Grafana dashboard shows real-time KMS metrics
- Alert runbook includes troubleshooting steps

**Dependencies:** Issue #2 (crypto package with metrics hooks)

---

### Issue #6: Documentation Updates
**Labels**: `documentation`, `priority:medium`  
**Milestone**: Feature Completion  
**Estimated Effort**: 0.5 days

**Description:**
Update all relevant documentation to reflect KMS envelope encryption support.

**Tasks:**
- [ ] Update `docs/CONFIG.md` with new environment variables
  - `KMS_KEY_ID`: AWS KMS key ID or alias (e.g., `alias/vod-tender-prod`)
  - Document priority: `KMS_KEY_ID` > `ENCRYPTION_KEY` > plaintext
  - Add examples for different environments (dev, staging, prod)
- [ ] Update `docs/SECURITY.md` OAuth token encryption section
  - Add "Envelope Encryption (KMS)" subsection
  - Update architecture diagram to show KMS flow
  - Add threat mitigation comparison table (direct vs envelope)
  - Document key rotation procedures (automatic AWS rotation)
- [ ] Update `docs/SECURITY_HARDENING.md` secrets management section
  - Add KMS as recommended approach for production
  - Update cost-benefit analysis
  - Add compliance benefits (SOC 2, HIPAA, PCI DSS)
- [ ] Update `docs/OPERATIONS.md` with operational procedures
  - Key rotation procedure (manual alias update)
  - Disaster recovery (KMS unavailable scenarios)
  - Migration procedure (link to Issue #4 docs)
- [ ] Create `docs/RUNBOOKS.md` section for KMS troubleshooting
  - Common errors: IAM permission denied, KMS key not found, rate limiting
  - Resolution steps for each error type
  - Emergency procedure: Rollback to direct-AES
- [ ] Update `README.md` security features section
  - Mention KMS envelope encryption support
  - Link to TOKEN_ENCRYPTION_ENVELOPE_DESIGN.md

**Acceptance Criteria:**
- All documentation references updated consistently
- Examples include both development (direct-AES) and production (KMS) configs
- Troubleshooting guides cover common KMS errors
- Links to design document from relevant docs

**Dependencies:** None (can start anytime)

---

### Issue #7: Staging Deployment & Validation
**Labels**: `deployment`, `testing`, `priority:high`  
**Milestone**: Feature Completion  
**Estimated Effort**: 1 week (includes monitoring period)

**Description:**
Deploy KMS envelope encryption to staging environment and validate all functionality.

**Tasks:**
- [ ] **Week 1: Deploy dual-mode code**
  - Deploy code from Issues #2 and #3 to staging
  - Keep using direct-AES initially (`ENCRYPTION_KEY` set, no `KMS_KEY_ID`)
  - Verify existing tokens still work (backward compatibility)
- [ ] **Week 1: Enable KMS for new tokens**
  - Set `KMS_KEY_ID=alias/vod-tender-staging` in staging config
  - Keep `ENCRYPTION_KEY` set (for reading old tokens)
  - Trigger token refresh (force OAuth re-authentication)
  - Verify new tokens stored with `encryption_version=2`
- [ ] **Week 1: Run migration script**
  - Execute migration script (Issue #4) in dry-run mode
  - Review output, ensure all tokens would be migrated
  - Execute migration script for real
  - Verify: `SELECT encryption_version FROM oauth_tokens` shows only version=2
- [ ] **Week 1: Remove legacy key**
  - Remove `ENCRYPTION_KEY` from staging config
  - Restart application
  - Verify tokens still decrypt successfully (using KMS only)
- [ ] **Week 2: Performance testing**
  - Measure token refresh latency (before vs after)
  - Verify cache hit rate is >90%
  - Load test: simulate 100 concurrent token decrypts
  - Measure p50, p99 latency under load
- [ ] **Week 2: Failure scenario testing**
  - Simulate KMS unavailable (block KMS endpoint in network policy)
  - Verify application degrades gracefully (logs errors, doesn't crash)
  - Restore KMS connectivity, verify recovery
  - Test IAM permission denied scenario
  - Test CMK disabled scenario
- [ ] **Week 2: Monitoring validation**
  - Verify Prometheus metrics are scraped
  - Verify Grafana dashboard displays data
  - Trigger CloudWatch alarm (manually cause KMS error)
  - Verify alert fires and reaches oncall

**Acceptance Criteria:**
- All tokens in staging encrypted with KMS envelope encryption
- No errors or degraded functionality observed for 1 week
- Performance impact <10% on token refresh operations
- All monitoring and alerting working correctly
- Team comfortable with operational procedures

**Dependencies:** Issues #1-6 (all prior work)

---

### Issue #8: Production Rollout
**Labels**: `deployment`, `production`, `priority:high`  
**Milestone**: Feature Completion  
**Estimated Effort**: 1 week (includes monitoring period)

**Description:**
Roll out KMS envelope encryption to production with zero downtime.

**Tasks:**
- [ ] **Pre-deployment checklist**
  - [ ] Staging validation complete (Issue #7)
  - [ ] Production KMS CMK created (Issue #1)
  - [ ] IAM policies tested in staging
  - [ ] Rollback plan documented
  - [ ] Team briefed on change (change management approval)
  - [ ] Maintenance window scheduled (optional, for safety)
- [ ] **Day 1: Deploy dual-mode code**
  - Deploy code to production (gradual rollout: 10% → 50% → 100%)
  - Monitor error rates, latency, logs
  - Keep `ENCRYPTION_KEY` set, no `KMS_KEY_ID` yet
  - Verify backward compatibility (version=1 tokens still work)
- [ ] **Day 2: Enable KMS for new tokens**
  - Set `KMS_KEY_ID=alias/vod-tender-prod` in production config
  - Rolling restart of pods (gradual: 1 pod at a time)
  - Monitor for KMS errors (IAM, connectivity)
  - Trigger token refresh (wait for automatic refresh or force)
  - Verify new tokens have `encryption_version=2`
- [ ] **Day 3: Run migration script**
  - Execute migration script in dry-run mode
  - Review output with team
  - Execute migration script for real (during low-traffic window)
  - Monitor for errors during migration
  - Verify migration complete: Query `encryption_version` distribution
- [ ] **Day 4-7: Monitoring period**
  - Monitor KMS metrics (latency, error rate, cache hit rate)
  - Monitor application logs for decrypt errors
  - Monitor CloudWatch alarms
  - Collect performance data (compare to baseline)
  - No action unless issues detected
- [ ] **Day 8: Remove legacy key**
  - Remove `ENCRYPTION_KEY` from production config
  - Rolling restart of pods
  - Verify tokens still decrypt (KMS-only mode)
  - Monitor for 24 hours
- [ ] **Day 9-14: Final validation**
  - Run end-to-end tests (VOD download, YouTube upload)
  - Verify token refresh working automatically
  - Check CloudTrail logs (audit all KMS decrypts)
  - Collect final performance metrics
  - Document any lessons learned

**Acceptance Criteria:**
- Zero downtime during migration
- All production tokens encrypted with KMS
- No increase in error rates or significant performance degradation
- CloudTrail logs show all decrypt operations
- Team comfortable with new operational model
- Post-migration review completed

**Dependencies:** Issue #7 (staging validation complete)

---

## Success Metrics

Track these metrics to validate successful implementation:

### Security Metrics
- [ ] 100% of production tokens encrypted with KMS envelope encryption
- [ ] 0 secrets (ENCRYPTION_KEY) in environment variables
- [ ] CloudTrail logs capture 100% of KMS decrypt operations
- [ ] IAM policies follow least privilege (only decrypt permission)

### Performance Metrics
- [ ] Token refresh latency increase <10% (baseline: ~500ms, target: <550ms)
- [ ] KMS decrypt p99 latency <200ms
- [ ] DEK cache hit rate >90%
- [ ] No increase in error rates during/after migration

### Operational Metrics
- [ ] Zero incidents during production rollout
- [ ] Documentation complete and accurate (validated by team)
- [ ] Team trained on KMS operational procedures
- [ ] Monitoring dashboards deployed and reviewed weekly

### Compliance Metrics
- [ ] SOC 2 audit requirement satisfied (centralized key management)
- [ ] HIPAA compliance improved (HSM-backed encryption)
- [ ] Key rotation procedure documented and tested

---

## Rollback Plan

If issues are discovered during production rollout:

### Immediate Rollback (Minutes)
1. Re-enable `ENCRYPTION_KEY` in production config
2. Rolling restart of pods
3. Tokens remain readable (dual-mode supports both formats)
4. No data loss (encrypted tokens still in database)

### Full Rollback (Hours)
1. Remove `KMS_KEY_ID` from production config
2. Deploy previous code version (without KMS support)
3. Tokens automatically written in direct-AES format going forward
4. Existing version=2 tokens remain (readable in dual-mode)
5. Schedule migration back to version=1 if needed (rare)

### Rollback Triggers
- KMS decrypt error rate >5%
- Token refresh success rate <95%
- Application crashes or memory leaks
- Performance degradation >20%
- Team consensus to abort

---

## Timeline Summary

| Week | Phase | Tasks |
|------|-------|-------|
| Week 1 | Infrastructure | Issue #1: KMS setup (1 day) |
| Week 2 | Development | Issues #2-4: Code implementation (3.5 days) |
| Week 3 | Monitoring & Docs | Issues #5-6: Observability and docs (1.5 days) |
| Week 4-5 | Staging Validation | Issue #7: Deploy and validate in staging (1 week) |
| Week 6-7 | Production Rollout | Issue #8: Gradual rollout to production (1 week) |
| Week 8 | Post-Mortem | Final validation, lessons learned, celebrate! 🎉 |

**Total Duration**: 8 weeks (assumes part-time effort, can compress to 4 weeks with full-time dedication)

---

## Notes for Implementation

### Priority Guidance
- **Must Have (P0)**: Issues #1-3, #7-8 (core functionality and deployment)
- **Should Have (P1)**: Issues #4-5 (migration script and monitoring)
- **Nice to Have (P2)**: Issue #6 (documentation, can be done in parallel)

### Risk Mitigation
- **Risk**: KMS unavailable during rollout → **Mitigation**: Gradual rollout, keep dual-mode for 1 week
- **Risk**: Performance degradation → **Mitigation**: DEK caching, load testing in staging first
- **Risk**: Migration script fails → **Mitigation**: Dry-run mode, transaction per token, rollback plan
- **Risk**: Team unfamiliar with KMS → **Mitigation**: Training session before rollout, runbooks

### Testing Strategy
- Unit tests: Mock KMS client for deterministic tests
- Integration tests: Use real KMS in staging (gated by env var)
- Performance tests: Benchmark encrypt/decrypt, load test token refresh
- Chaos testing: Simulate KMS unavailable, IAM denied, rate limiting
- Security testing: Verify no DEKs in logs, audit CloudTrail logs

---

**Last Updated**: 2025-10-26  
**Related Design**: TOKEN_ENCRYPTION_ENVELOPE_DESIGN.md  
**Status**: Ready for issue creation after design approval
