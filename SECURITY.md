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
- OAuth tokens encrypted at rest (optional feature)
- Parameterized queries for all database access
- Input validation and sanitization
- Network policies restrict egress
- Rate limiting on API endpoints

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
│  - Encrypted backups                             │
│  - Optional token encryption                     │
│  - Secure database connections                   │
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
