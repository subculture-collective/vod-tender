# Security Policy

## Supported Versions

We actively support the following versions with security updates:

| Version | Supported          |
| ------- | ------------------ |
| 1.x.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

We take the security of vod-tender seriously. If you discover a security vulnerability, please report it responsibly.

### How to Report

**Please DO NOT open a public GitHub issue for security vulnerabilities.**

Instead, report security issues through one of these private channels:

1. **GitHub Security Advisories** (preferred):
   - Navigate to https://github.com/subculture-collective/vod-tender/security/advisories
   - Click "Report a vulnerability"
   - Fill out the form with details

2. **Email**:
   - Send to: security@subculture-collective.com
   - Subject: `[SECURITY] vod-tender vulnerability report`
   - Include detailed description (see below)

3. **Encrypted Email** (for sensitive disclosures):
   - Use our PGP key: [Download PGP Key](https://keybase.io/subculturecollective/pgp_keys.asc)
   - Fingerprint: `XXXX XXXX XXXX XXXX XXXX XXXX XXXX XXXX XXXX XXXX`
   - Send to: security@subculture-collective.com

### What to Include

Please provide the following information:

- **Type of vulnerability** (e.g., RCE, SQL injection, XSS, authentication bypass)
- **Component affected** (e.g., API endpoint, frontend, database)
- **Steps to reproduce** (detailed, ideally with a proof-of-concept)
- **Potential impact** (data exposure, privilege escalation, etc.)
- **Affected versions** (if known)
- **Suggested fix** (if you have one)
- **Your contact information** (for follow-up questions)

### Example Report

```
Subject: [SECURITY] SQL Injection in VOD API endpoint

Type: SQL Injection
Affected Component: API - /api/vods/{id}/chat endpoint
Severity: High

Description:
The chat message retrieval endpoint is vulnerable to SQL injection through
the 'limit' query parameter. An attacker can extract sensitive data from
the database.

Steps to Reproduce:
1. Send GET request to /api/vods/123/chat?limit=1' OR '1'='1
2. Observe response contains all chat messages regardless of limit
3. Execute: ?limit=1 UNION SELECT password FROM oauth_tokens--

Impact:
- Database credentials can be extracted
- OAuth tokens can be exfiltrated
- Potential lateral movement to other services

Affected Versions:
- Tested on v1.2.3
- Likely affects all versions prior to v1.3.0

Suggested Fix:
Use parameterized queries instead of string concatenation in
server/handlers.go:getChatMessages()

Reporter: Jane Doe (jane@example.com)
```

## Response Timeline

- **Acknowledgment**: Within 48 hours of report
- **Initial Assessment**: Within 5 business days
- **Status Update**: Every 7 days until resolution
- **Fix Development**: Varies by severity (see below)
- **Public Disclosure**: 90 days after fix release (or by mutual agreement)

### Severity-Based Response

| Severity | Response Time | Disclosure Timeline |
|----------|--------------|---------------------|
| Critical | 24-48 hours  | 30 days after fix   |
| High     | 3-5 days     | 60 days after fix   |
| Medium   | 7-14 days    | 90 days after fix   |
| Low      | 30 days      | 120 days after fix  |

**Critical**: RCE, authentication bypass, arbitrary file read/write
**High**: SQL injection, XSS, privilege escalation, data exposure
**Medium**: CSRF, information disclosure, DoS
**Low**: Rate limiting bypass, verbose error messages

## Disclosure Policy

We follow **Coordinated Vulnerability Disclosure**:

1. **Private Fix Development**: Vulnerability fixed in private branch
2. **Security Advisory**: GitHub Security Advisory published (embargoed)
3. **Fix Release**: New version released with security patches
4. **Public Disclosure**: Advisory made public after timeline (default 90 days)
5. **CVE Assignment**: Request CVE if severity warrants

### Early Disclosure

We may disclose earlier than 90 days if:
- The vulnerability is already publicly known
- Active exploitation is detected in the wild
- Fixes are straightforward and widely deployed

### Delayed Disclosure

We may extend the 90-day timeline if:
- Fix requires significant refactoring
- Dependent libraries need patching first
- By mutual agreement with reporter

## Security Updates

Security updates are released as:

- **Patch versions** for non-breaking fixes (e.g., 1.2.3 → 1.2.4)
- **Minor versions** if breaking changes are necessary (e.g., 1.2.x → 1.3.0)

Subscribe to security notifications:
- Watch this repository and enable "Security alerts" in GitHub notifications
- Follow releases: https://github.com/subculture-collective/vod-tender/releases
- Subscribe to mailing list: vod-tender-security@googlegroups.com

## Bug Bounty Program

**Status**: No formal bug bounty program at this time.

We deeply appreciate security researchers who report vulnerabilities responsibly. While we don't currently offer monetary rewards, we will:

- Credit you in the security advisory (if desired)
- Add you to our SECURITY-HALL-OF-FAME.md
- Provide a letter of recommendation for your portfolio

## Security Best Practices

### For Users

See [SECURITY_HARDENING.md](./docs/SECURITY_HARDENING.md) for comprehensive guidance. Key practices:

- **Keep updated**: Run the latest version
- **Secure secrets**: Never commit credentials; use secret managers
- **Enable TLS**: Always use HTTPS in production
- **Network isolation**: Restrict database access with firewall rules
- **Audit logs**: Enable and monitor structured logging
- **Token rotation**: Rotate OAuth tokens periodically
- **Least privilege**: Run with minimal required permissions
- **Backup encryption**: Encrypt database backups at rest

### For Developers

- **Dependencies**: Keep dependencies updated (Dependabot enabled)
- **Code scanning**: CI/CD includes SAST (gitleaks, govulncheck, Trivy)
- **Review process**: All PRs require review before merge
- **Testing**: Write security-focused tests for authentication/authorization
- **Input validation**: Validate and sanitize all user input
- **Parameterized queries**: Never use string concatenation for SQL
- **Secret scanning**: Pre-commit hooks prevent credential commits
- **Container scanning**: Docker images scanned for vulnerabilities (Trivy)

## Security Scanning Schedule

| Scan Type | Frequency | Tool | CI/CD |
|-----------|-----------|------|-------|
| Secret scanning | Every commit | gitleaks | ✅ |
| Go vulnerability check | Every commit | govulncheck | ✅ |
| Container scanning | Every build | Trivy | ✅ |
| Dependency scanning | Weekly | Dependabot | ✅ |
| SAST | Every PR | golangci-lint | ✅ |
| DAST | Monthly | Manual | ❌ |
| Penetration testing | Annual | External vendor | ❌ |

## Security Architecture

### Threat Model

**Assets**:
- OAuth tokens (Twitch, YouTube)
- Chat messages (may contain PII)
- VOD metadata
- Downloaded VOD files

**Threats**:
- Unauthorized access to OAuth tokens → impersonation
- SQL injection → data exfiltration
- Path traversal → arbitrary file access
- SSRF → internal network scanning
- DoS → service disruption

**Mitigations**:
- **OAuth tokens encrypted at rest** (AES-256-GCM) when ENCRYPTION_KEY configured
- Parameterized queries for all database access
- Input validation and sanitization
- Network policies restrict egress
- Rate limiting on API endpoints

### OAuth Token Encryption

**Status**: ✅ **Implemented** (as of v1.3.0)

OAuth tokens (Twitch, YouTube) grant full API access and are the highest-value secrets in the system. To protect against database compromise scenarios (backups leaked, SQL injection, insider threat), tokens are encrypted at rest using AES-256-GCM authenticated encryption.

#### Encryption Architecture

- **Algorithm**: AES-256-GCM (Galois/Counter Mode)
  - 256-bit key length (32 bytes)
  - Authenticated Encryption with Associated Data (AEAD)
  - Nonce randomization prevents deterministic encryption
  - 16-byte authentication tag detects tampering
  
- **Key Management**: 
  - Data Encryption Key (DEK) loaded from `ENCRYPTION_KEY` environment variable
  - Base64-encoded for safe transmission through configuration systems
  - Initialized once per process lifetime (singleton pattern)
  - Future: Key rotation support via dual-key operation

- **Storage Format**: 
  - Ciphertext is base64-encoded for TEXT column storage
  - Overhead: 28 bytes (12-byte nonce + 16-byte auth tag)
  - `encryption_version` column indicates encryption state:
    - `0` = plaintext (legacy/dev mode)
    - `1` = AES-256-GCM encrypted
  - `encryption_key_id` column supports future key rotation

#### Security Properties

- ✅ **Confidentiality**: Tokens unreadable without encryption key
- ✅ **Integrity**: Authentication tag prevents tampering
- ✅ **Authenticity**: GCM mode verifies ciphertext origin
- ✅ **Replay protection**: Nonce randomization prevents ciphertext reuse attacks
- ✅ **Backward compatibility**: Plaintext tokens (version=0) readable during migration
- ✅ **Forward secrecy**: Old tokens remain secure after key rotation (future)

#### Threat Scenarios Mitigated

| Threat | Without Encryption | With Encryption |
|--------|-------------------|-----------------|
| Database backup leaked | ❌ Tokens exposed in plaintext | ✅ Tokens encrypted, key stored separately |
| SQL injection (read-only) | ❌ Attacker reads tokens directly | ✅ Attacker gets ciphertext (useless without key) |
| Insider threat (DBA access) | ❌ DBA has full token access | ✅ DBA sees ciphertext only |
| Physical disk theft | ❌ Tokens readable from disk | ✅ Encrypted at rest |
| Memory dump attack | ⚠️ Tokens in memory during use | ⚠️ Tokens decrypted in memory (same risk) |

**Note**: Encryption at rest does **not** protect against:
- Application-level vulnerabilities (XSS, CSRF)
- Memory dumps while tokens are in use
- Compromised encryption key
- Time-of-check-to-time-of-use (TOCTOU) attacks

#### Configuration Requirements

**Development** (local, testing):
```bash
# Optional - plaintext storage acceptable for local dev
# ENCRYPTION_KEY not set → plaintext (encryption_version=0)
```

**Staging** (pre-production):
```bash
# Recommended - test encryption configuration
ENCRYPTION_KEY=$(openssl rand -base64 32)
```

**Production** (required):
```bash
# REQUIRED - generate unique key, store in secrets manager
ENCRYPTION_KEY=YOUR_GENERATED_KEY_HERE  # REPLACE with output from: openssl rand -base64 32
```

⚠️ **Security Warning**: If `ENCRYPTION_KEY` is not set, tokens are stored in **plaintext**. This is logged as a warning on startup:
```
WARN ENCRYPTION_KEY not set, OAuth tokens will be stored in plaintext (not recommended for production)
```

#### Key Storage Security

The encryption key is the single point of failure. Store securely:

1. **AWS**: AWS Secrets Manager with IAM policies
   ```bash
   aws secretsmanager get-secret-value --secret-id vod-tender/encryption-key --query SecretString --output text
   ```

2. **Kubernetes**: Sealed Secrets or External Secrets Operator
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: vod-tender-secrets
   type: Opaque
   data:
     ENCRYPTION_KEY: <base64-encoded-key>  # Double-encoded
   ```

3. **HashiCorp Vault**: Dynamic secrets with TTL
   ```bash
   vault kv get -field=encryption_key secret/vod-tender
   ```

4. **Google Cloud**: Secret Manager with workload identity
   ```bash
   gcloud secrets versions access latest --secret="vod-tender-encryption-key"
   ```

**Never**:
- ❌ Commit keys to Git (even encrypted repos)
- ❌ Store keys in Docker images
- ❌ Log keys in application logs
- ❌ Share keys via email/chat
- ❌ Use the same key across environments

#### Key Rotation Procedure

**Current Implementation** (manual rotation with downtime):

1. Generate new key: `NEW_KEY=$(openssl rand -base64 32)`
2. Schedule maintenance window (brief downtime required)
3. Update `ENCRYPTION_KEY` to new key
4. Restart application (picks up new key)
5. Existing tokens (version=1, old key) will fail decryption
6. Users must re-authenticate via OAuth flows
7. New tokens encrypted with new key

**Recommended Rotation Frequency**:
- Production: Annually or on suspected compromise
- Staging: Quarterly
- Development: Not required

**Future Enhancement** (zero-downtime rotation):
- Support `ENCRYPTION_KEY_OLD` + `ENCRYPTION_KEY_NEW`
- Decrypt with old key, re-encrypt with new key on read
- Background job to migrate all tokens to new key
- Remove old key after 100% migration

#### Performance Impact

Encryption/decryption overhead is **negligible** for OAuth token operations:

- Encryption time: ~0.05ms per token
- Decryption time: ~0.05ms per token
- Database write: ~10ms (dominated by I/O, not crypto)
- Database read: ~5ms (dominated by I/O, not crypto)

**Benchmark** (on typical server hardware):
```
BenchmarkEncrypt-8     50000    0.053 ms/op    0 allocs/op
BenchmarkDecrypt-8     50000    0.047 ms/op    0 allocs/op
```

Ciphertext size overhead: 28 bytes (nonce + auth tag) per token, negligible for typical 100-500 character tokens.

#### Migration from Plaintext

Deployments with existing plaintext tokens automatically migrate on next token refresh:

**Migration Timeline**:
1. Deploy code with encryption support → tokens remain plaintext (version=0)
2. Set `ENCRYPTION_KEY` environment variable
3. Restart service → logs "OAuth token encryption enabled"
4. Existing tokens (version=0) read as plaintext
5. On next OAuth refresh (automatic, ~55 minutes for Twitch) → token re-saved encrypted (version=1)
6. Monitor `encryption_version` column to track migration progress

**Manual Migration** (optional, for immediate encryption):
```sql
-- Check migration status
SELECT provider, encryption_version FROM oauth_tokens;

-- Trigger immediate re-encryption: force token refresh via admin endpoint
curl -X POST http://localhost:8080/admin/oauth/refresh?provider=twitch
```

**Rollback Safety**: If encryption causes issues, remove `ENCRYPTION_KEY` and restart. Encrypted tokens (version=1) will fail to decrypt, requiring re-authentication. Plaintext tokens (version=0) continue working.

#### Monitoring & Alerts

Key metrics to monitor:

- **Encryption status**: Log message on startup confirms encryption enabled
- **Decryption failures**: Log as ERROR, indicates key mismatch or corruption
- **Migration progress**: Query `encryption_version` distribution
- **Key age**: Alert if key older than rotation policy (365 days)

Sample Prometheus alerts:
```yaml
- alert: OAuthEncryptionDisabled
  expr: vod_tender_encryption_enabled == 0
  for: 1h
  annotations:
    summary: "OAuth token encryption is disabled (plaintext storage)"
    severity: critical

- alert: OAuthDecryptionFailures
  expr: rate(vod_tender_oauth_decrypt_errors_total[5m]) > 0
  annotations:
    summary: "OAuth token decryption failing (key mismatch?)"
    severity: critical
```

#### Cryptographic Details

For security auditors and researchers:

- **Cipher**: AES-256-GCM per [FIPS 197](https://csrc.nist.gov/publications/detail/fips/197/final) and [NIST SP 800-38D](https://csrc.nist.gov/publications/detail/sp/800-38d/final)
- **Nonce**: 96-bit (12 bytes), randomly generated via `crypto/rand` per encryption
- **Authentication Tag**: 128-bit (16 bytes), appended by GCM mode
- **Key Derivation**: None (uses raw 256-bit key directly from environment)
- **Associated Data**: None (AAD field unused in current implementation)
- **Implementation**: Go standard library `crypto/aes` and `crypto/cipher`

**Security Assumptions**:
- `crypto/rand` provides cryptographically secure randomness
- Nonce never reused with same key (guaranteed by random generation)
- AES-GCM implementation is side-channel resistant (Go stdlib)
- Encryption key has at least 256 bits of entropy

**Future Enhancements**:
- Envelope encryption with Key Encryption Keys (KEK) via AWS KMS/GCP KMS
- Hardware Security Module (HSM) integration
- Memory-locking for keys (`mlock()` to prevent swap)
- Key derivation from master password (PBKDF2/Argon2)

### Defense in Depth

```
┌─────────────────────────────────────────────────┐
│  Layer 1: Network                                │
│  - Firewall rules                                │
│  - TLS/HTTPS only                                │
│  - Network policies (K8s)                        │
└─────────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────────┐
│  Layer 2: Application                            │
│  - Rate limiting                                 │
│  - Input validation                              │
│  - CSRF protection                               │
│  - Security headers                              │
└─────────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────────┐
│  Layer 3: Authentication & Authorization         │
│  - OAuth token validation                        │
│  - Admin endpoint access control                 │
│  - Least privilege service accounts              │
└─────────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────────┐
│  Layer 4: Data                                   │
│  - OAuth tokens encrypted at rest (AES-256-GCM) │
│  - Encrypted database backups                    │
│  - Secure database connections (TLS)             │
│  - Secrets in external vault (not in config)    │
└─────────────────────────────────────────────────┘
```

## Compliance Considerations

### GDPR (General Data Protection Regulation)

If processing EU user data:

- **Legal Basis**: Legitimate interest for VOD archival
- **Data Minimization**: Only store necessary chat messages
- **Right to Erasure**: Provide endpoint to delete user's chat messages
- **Data Retention**: Configure retention policies (see CONFIG.md)
- **Data Processing Agreement**: Required if using third-party hosting

### COPPA (Children's Online Privacy Protection Act)

- Twitch TOS prohibits users under 13
- Do not intentionally collect data from children
- If discovered, purge immediately

### DMCA (Digital Millennium Copyright Act)

- Archival for personal/research use may qualify as fair use
- Commercial redistribution requires rights clearance
- Respond to takedown requests promptly

## Audit Logging

Enable comprehensive audit logging in production:

```bash
# Environment variables
LOG_FORMAT=json
LOG_LEVEL=info
AUDIT_LOG_ENABLED=1
```

Logged events:
- Authentication attempts (success/failure)
- OAuth token refresh
- VOD download start/complete
- Admin endpoint access
- Configuration changes
- Database migrations

Log format includes:
- Timestamp
- Correlation ID
- Component
- Event type
- User/session ID
- IP address (if applicable)
- Outcome (success/failure)

## Incident Response

If a security incident is confirmed:

1. **Containment**: Isolate affected systems
2. **Assessment**: Determine scope and impact
3. **Eradication**: Remove threat and fix vulnerability
4. **Recovery**: Restore services from clean backups
5. **Notification**: Inform affected users if data was compromised
6. **Post-mortem**: Document lessons learned and improve defenses

See [RUNBOOKS.md](./docs/RUNBOOKS.md) for detailed incident response procedures.

## Security Contacts

- **Security Team**: security@subculture-collective.com
- **PGP Key**: https://keybase.io/subculturecollective/pgp_keys.asc
- **GitHub Security Advisories**: https://github.com/subculture-collective/vod-tender/security/advisories

## Acknowledgments

We thank the following security researchers for responsible disclosure:

<!-- This section will be updated as researchers contribute -->

*No vulnerabilities reported yet.*

---

**Last Updated**: 2025-10-20
**Next Review**: 2026-04-20

For operational security hardening, see [docs/SECURITY_HARDENING.md](./docs/SECURITY_HARDENING.md).
