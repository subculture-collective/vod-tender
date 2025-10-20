# Security Hardening Guide

This guide provides a comprehensive checklist and best practices for securing vod-tender in production environments.

## Table of Contents

- [Pre-Deployment Security Checklist](#pre-deployment-security-checklist)
- [Network Security](#network-security)
- [Application Security](#application-security)
- [Database Security](#database-security)
- [Secrets Management](#secrets-management)
- [Container Security](#container-security)
- [Monitoring and Auditing](#monitoring-and-auditing)
- [Security Benchmarks](#security-benchmarks)

## Pre-Deployment Security Checklist

Use this checklist before deploying to production:

### Essential (Must Have)

- [ ] **TLS/HTTPS enabled** for all external endpoints
  - [ ] Valid SSL certificates installed
  - [ ] HTTP redirects to HTTPS
  - [ ] TLS 1.2+ only (disable TLS 1.0, 1.1)
  
- [ ] **Secrets properly managed**
  - [ ] No credentials in environment variables (visible in `docker inspect`)
  - [ ] Secrets stored in vault/secrets manager
  - [ ] Database passwords rotated from defaults
  
- [ ] **Database access restricted**
  - [ ] Firewall rules limit access to application only
  - [ ] Strong password set (20+ characters, random)
  - [ ] SSL/TLS required for database connections
  
- [ ] **OAuth token encryption enabled**
  - [ ] `TOKEN_ENCRYPTION_KEY` set (32+ byte random key)
  - [ ] Tokens encrypted at rest in database
  
- [ ] **Rate limiting configured**
  - [ ] API endpoints rate limited (10-100 req/min)
  - [ ] Protection against brute force attacks
  
- [ ] **Security headers set**
  - [ ] `Strict-Transport-Security` (HSTS)
  - [ ] `X-Frame-Options: SAMEORIGIN`
  - [ ] `X-Content-Type-Options: nosniff`
  - [ ] `X-XSS-Protection: 1; mode=block`
  - [ ] `Content-Security-Policy` configured
  
- [ ] **Audit logging enabled**
  - [ ] `LOG_FORMAT=json` for structured logs
  - [ ] Authentication events logged
  - [ ] Admin actions logged
  - [ ] Logs sent to centralized system (SIEM)

### Recommended (Should Have)

- [ ] **Network isolation**
  - [ ] Application in private subnet
  - [ ] Database in separate subnet
  - [ ] Network policies/security groups configured
  
- [ ] **Read-only filesystem**
  - [ ] Container root filesystem read-only
  - [ ] Writable volumes only for `/data` and `/tmp`
  
- [ ] **Non-root user**
  - [ ] Application runs as unprivileged user
  - [ ] UID/GID > 1000
  - [ ] No unnecessary capabilities
  
- [ ] **Minimal attack surface**
  - [ ] Debug endpoints disabled in production
  - [ ] Unnecessary services removed from container
  - [ ] Minimal base image (alpine, distroless)
  
- [ ] **Backup encryption**
  - [ ] Database backups encrypted at rest
  - [ ] Encrypted in transit to offsite storage
  - [ ] Tested restore procedure
  
- [ ] **Token rotation**
  - [ ] OAuth tokens rotated every 90 days
  - [ ] Database credentials rotated quarterly
  - [ ] API keys rotated on schedule
  
- [ ] **Vulnerability scanning**
  - [ ] Container images scanned (Trivy, Clair)
  - [ ] Dependencies checked for CVEs (Dependabot)
  - [ ] SAST tools integrated in CI/CD

### Advanced (Nice to Have)

- [ ] **Web Application Firewall (WAF)**
  - [ ] SQL injection protection
  - [ ] XSS filtering
  - [ ] Rate limiting at edge
  
- [ ] **Intrusion Detection System (IDS)**
  - [ ] Anomaly detection on network traffic
  - [ ] File integrity monitoring
  - [ ] Alerting on suspicious activity
  
- [ ] **Zero Trust Architecture**
  - [ ] Service mesh with mTLS (Istio, Linkerd)
  - [ ] Certificate-based authentication
  - [ ] Least privilege service accounts
  
- [ ] **Security Information and Event Management (SIEM)**
  - [ ] Centralized log aggregation
  - [ ] Correlation rules for attack patterns
  - [ ] Automated alerting
  
- [ ] **Penetration Testing**
  - [ ] Annual third-party security assessment
  - [ ] Quarterly internal testing
  - [ ] Remediation plan for findings

## Network Security

### Firewall Rules

**Kubernetes Network Policies**:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: vod-api-strict
  namespace: vod-tender
spec:
  podSelector:
    matchLabels:
      app: vod-api
  policyTypes:
  - Ingress
  - Egress
  ingress:
  # Allow from ingress controller only
  - from:
    - namespaceSelector:
        matchLabels:
          name: ingress-nginx
    ports:
    - protocol: TCP
      port: 8080
  egress:
  # Allow to database
  - to:
    - podSelector:
        matchLabels:
          app: postgres
    ports:
    - protocol: TCP
      port: 5432
  # Allow to Twitch/YouTube APIs (443)
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443
  # Allow DNS
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: UDP
      port: 53
```

**iptables (Bare Metal)**:

```bash
# /etc/iptables/rules.v4

*filter
:INPUT DROP [0:0]
:FORWARD DROP [0:0]
:OUTPUT ACCEPT [0:0]

# Allow loopback
-A INPUT -i lo -j ACCEPT

# Allow established connections
-A INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT

# Allow SSH (from specific IP)
-A INPUT -p tcp --dport 22 -s 203.0.113.0/24 -j ACCEPT

# Allow HTTPS (from load balancer)
-A INPUT -p tcp --dport 443 -s 10.0.1.0/24 -j ACCEPT

# Allow Postgres (from app server only)
-A INPUT -p tcp --dport 5432 -s 10.0.2.10 -j ACCEPT

# Drop everything else
-A INPUT -j DROP

COMMIT
```

**AWS Security Groups**:

```hcl
# API security group
resource "aws_security_group" "vod_api" {
  name        = "vod-api"
  description = "VOD Tender API"
  vpc_id      = aws_vpc.main.id

  ingress {
    description     = "HTTP from ALB"
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }

  egress {
    description = "Postgres"
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    security_groups = [aws_security_group.postgres.id]
  }

  egress {
    description = "HTTPS to internet"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# Database security group
resource "aws_security_group" "postgres" {
  name        = "vod-postgres"
  description = "VOD Tender Database"
  vpc_id      = aws_vpc.main.id

  ingress {
    description     = "Postgres from API"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.vod_api.id]
  }

  # No egress rules (deny all outbound)
}
```

### TLS Configuration

**Nginx TLS Hardening**:

```nginx
# /etc/nginx/conf.d/ssl-params.conf

ssl_protocols TLSv1.2 TLSv1.3;
ssl_ciphers 'ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384';
ssl_prefer_server_ciphers off;

ssl_session_timeout 1d;
ssl_session_cache shared:SSL:10m;
ssl_session_tickets off;

# OCSP stapling
ssl_stapling on;
ssl_stapling_verify on;
resolver 8.8.8.8 8.8.4.4 valid=300s;
resolver_timeout 5s;

# Security headers
add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;
add_header X-Frame-Options "SAMEORIGIN" always;
add_header X-Content-Type-Options "nosniff" always;
add_header X-XSS-Protection "1; mode=block" always;
add_header Referrer-Policy "no-referrer-when-downgrade" always;
```

Test TLS configuration:

```bash
# Using testssl.sh
testssl.sh https://vod-api.example.com

# Using SSL Labs
# Visit: https://www.ssllabs.com/ssltest/analyze.html?d=vod-api.example.com
```

### Rate Limiting

**Nginx**:

```nginx
# Rate limiting zones
limit_req_zone $binary_remote_addr zone=api_general:10m rate=10r/s;
limit_req_zone $binary_remote_addr zone=api_auth:10m rate=5r/m;
limit_req_zone $binary_remote_addr zone=api_admin:10m rate=2r/m;

server {
    # General API endpoints
    location /api/ {
        limit_req zone=api_general burst=20 nodelay;
        limit_req_status 429;
        proxy_pass http://vod_api;
    }

    # Authentication endpoints
    location /auth/ {
        limit_req zone=api_auth burst=5 nodelay;
        proxy_pass http://vod_api;
    }

    # Admin endpoints
    location /admin/ {
        limit_req zone=api_admin burst=2 nodelay;
        
        # IP whitelist
        allow 10.0.0.0/8;
        deny all;
        
        proxy_pass http://vod_api;
    }
}
```

**Application-Level** (future enhancement):

```go
// middleware/ratelimit.go
import "golang.org/x/time/rate"

func RateLimitMiddleware(limiter *rate.Limiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !limiter.Allow() {
                http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

## Application Security

### Security Headers

Configure in reverse proxy or application:

```yaml
# Kubernetes Ingress annotations
metadata:
  annotations:
    nginx.ingress.kubernetes.io/configuration-snippet: |
      more_set_headers "Strict-Transport-Security: max-age=31536000; includeSubDomains";
      more_set_headers "X-Frame-Options: SAMEORIGIN";
      more_set_headers "X-Content-Type-Options: nosniff";
      more_set_headers "X-XSS-Protection: 1; mode=block";
      more_set_headers "Referrer-Policy: strict-origin-when-cross-origin";
      more_set_headers "Permissions-Policy: geolocation=(), microphone=(), camera=()";
```

**Content Security Policy**:

```
Content-Security-Policy: 
  default-src 'self'; 
  script-src 'self' 'unsafe-inline' 'unsafe-eval'; 
  style-src 'self' 'unsafe-inline'; 
  img-src 'self' data: https:; 
  font-src 'self' data:; 
  connect-src 'self' https://vod-api.example.com; 
  frame-ancestors 'none'; 
  base-uri 'self'; 
  form-action 'self';
```

### Input Validation

All user input must be validated and sanitized:

```go
// Example: Validate VOD ID parameter
func validateVODID(id string) error {
    // Only allow alphanumeric and hyphens
    matched, _ := regexp.MatchString(`^[a-zA-Z0-9\-]+$`, id)
    if !matched {
        return errors.New("invalid VOD ID format")
    }
    if len(id) > 100 {
        return errors.New("VOD ID too long")
    }
    return nil
}

// Example: Sanitize chat message before storage
func sanitizeMessage(msg string) string {
    // Remove null bytes
    msg = strings.ReplaceAll(msg, "\x00", "")
    // Limit length
    if len(msg) > 500 {
        msg = msg[:500]
    }
    return msg
}
```

### SQL Injection Prevention

**Always use parameterized queries**:

```go
// ✅ GOOD: Parameterized query
rows, err := db.Query(
    "SELECT * FROM vods WHERE twitch_vod_id = $1 AND processed = $2",
    vodID,
    false,
)

// ❌ BAD: String concatenation
query := fmt.Sprintf("SELECT * FROM vods WHERE twitch_vod_id = '%s'", vodID)
rows, err := db.Query(query)
```

### CSRF Protection

For state-changing endpoints:

```go
// Use gorilla/csrf or similar
import "github.com/gorilla/csrf"

func main() {
    csrfMiddleware := csrf.Protect(
        []byte("32-byte-long-random-key"),
        csrf.Secure(true),
        csrf.HttpOnly(true),
    )
    
    http.Handle("/", csrfMiddleware(handler))
}
```

## Database Security

### Connection Security

**Require SSL/TLS**:

```bash
# Environment variable
DB_DSN="postgres://vod:password@postgres:5432/vod?sslmode=require"
```

**PostgreSQL Configuration** (`postgresql.conf`):

```ini
# Require SSL
ssl = on
ssl_cert_file = '/etc/ssl/certs/server.crt'
ssl_key_file = '/etc/ssl/private/server.key'
ssl_ca_file = '/etc/ssl/certs/ca.crt'

# Authentication
password_encryption = scram-sha-256

# Logging
log_connections = on
log_disconnections = on
log_duration = on
log_statement = 'mod'  # Log DDL and DML

# Resource limits
max_connections = 100
shared_buffers = 256MB
effective_cache_size = 1GB
```

**pg_hba.conf** (host-based authentication):

```
# TYPE  DATABASE        USER            ADDRESS                 METHOD
local   all             postgres                                peer
host    all             all             127.0.0.1/32            scram-sha-256
host    vod             vod             10.0.2.0/24             scram-sha-256 clientcert=1
host    all             all             ::1/128                 scram-sha-256
```

### Principle of Least Privilege

Create separate database users for different purposes:

```sql
-- Application user (read/write to vod tables only)
CREATE ROLE vod_app WITH LOGIN PASSWORD 'strong_password';
GRANT CONNECT ON DATABASE vod TO vod_app;
GRANT USAGE ON SCHEMA public TO vod_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO vod_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO vod_app;

-- Read-only user (for analytics/monitoring)
CREATE ROLE vod_readonly WITH LOGIN PASSWORD 'strong_password';
GRANT CONNECT ON DATABASE vod TO vod_readonly;
GRANT USAGE ON SCHEMA public TO vod_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO vod_readonly;

-- Backup user (for pg_dump)
CREATE ROLE vod_backup WITH LOGIN PASSWORD 'strong_password';
GRANT CONNECT ON DATABASE vod TO vod_backup;
GRANT USAGE ON SCHEMA public TO vod_backup;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO vod_backup;
```

### Data Encryption

**Encryption at Rest**:

- Use encrypted storage volumes (LUKS, AWS EBS encryption, GCP persistent disk encryption)
- Enable PostgreSQL transparent data encryption (if using enterprise version)

**Token Encryption** (application-level):

```bash
# Generate encryption key
openssl rand -base64 32

# Set environment variable
TOKEN_ENCRYPTION_KEY=<generated-key>
```

Tokens are encrypted before storage using AES-256-GCM.

### Backup Security

```bash
# Encrypt backup with GPG
pg_dump -U vod vod | gzip | gpg --encrypt --recipient admin@example.com > vod_backup.sql.gz.gpg

# Decrypt and restore
gpg --decrypt vod_backup.sql.gz.gpg | gunzip | psql -U vod vod
```

**Automated encrypted backups**:

```bash
#!/bin/bash
# /opt/vod-tender/bin/secure-backup.sh

set -euo pipefail

BACKUP_DIR=/opt/vod-tender/backups
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
GPG_RECIPIENT="admin@example.com"

# Dump and encrypt
pg_dump -U vod vod | \
  gzip | \
  gpg --encrypt --recipient "$GPG_RECIPIENT" \
  > "${BACKUP_DIR}/vod_${TIMESTAMP}.sql.gz.gpg"

# Upload to S3 with server-side encryption
aws s3 cp "${BACKUP_DIR}/vod_${TIMESTAMP}.sql.gz.gpg" \
  s3://vod-tender-backups/ \
  --server-side-encryption AES256

# Remove local backup older than 7 days
find "${BACKUP_DIR}" -name "*.gpg" -mtime +7 -delete
```

## Secrets Management

### Environment Variables (Least Secure)

**Avoid** storing secrets in plain environment variables:

```bash
# ❌ BAD: Visible in docker inspect, process list
docker run -e DB_PASSWORD=secret123 vod-tender
```

### Docker Secrets (Better)

```bash
# Create secret
echo "secret_password" | docker secret create db_password -

# Use in compose
services:
  api:
    secrets:
      - db_password
    environment:
      DB_PASSWORD_FILE: /run/secrets/db_password
```

### Kubernetes Secrets (Better)

```bash
# Create secret
kubectl create secret generic vod-creds \
  --from-literal=DB_PASSWORD=secret_password \
  -n vod-tender

# Use in pod
env:
- name: DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: vod-creds
      key: DB_PASSWORD
```

### External Secrets Operator (Best)

Integrate with cloud secret managers:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: vod-creds
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: SecretStore
  target:
    name: vod-creds
  data:
  - secretKey: DB_PASSWORD
    remoteRef:
      key: vod-tender/database
      property: password
```

### HashiCorp Vault (Best)

```bash
# Store secret in Vault
vault kv put secret/vod-tender/database password=secret_password

# Application retrieves at runtime
vault kv get -field=password secret/vod-tender/database
```

### Secret Rotation

Automate credential rotation:

```bash
#!/bin/bash
# /opt/vod-tender/bin/rotate-db-password.sh

NEW_PASSWORD=$(openssl rand -base64 32)

# Update database
psql -U postgres -c "ALTER ROLE vod PASSWORD '$NEW_PASSWORD';"

# Update secret in vault
kubectl create secret generic vod-creds \
  --from-literal=DB_PASSWORD="$NEW_PASSWORD" \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart application to pick up new secret
kubectl rollout restart deployment/vod-api -n vod-tender
```

Schedule quarterly:

```bash
# Cron job
0 2 1 */3 * /opt/vod-tender/bin/rotate-db-password.sh
```

## Container Security

### Minimal Base Images

```dockerfile
# ❌ AVOID: Large attack surface
FROM ubuntu:latest

# ✅ BETTER: Minimal image
FROM alpine:3.18

# ✅ BEST: Distroless
FROM gcr.io/distroless/static-debian12
```

### Non-Root User

```dockerfile
# Create unprivileged user
RUN addgroup -g 10001 vod && \
    adduser -D -u 10001 -G vod vod

# Switch to non-root
USER vod

# Run application
CMD ["/app/vod-tender"]
```

### Read-Only Root Filesystem

```yaml
# Kubernetes
spec:
  containers:
  - name: api
    securityContext:
      readOnlyRootFilesystem: true
    volumeMounts:
    - name: tmp
      mountPath: /tmp
    - name: data
      mountPath: /data
  volumes:
  - name: tmp
    emptyDir: {}
```

### Drop Capabilities

```yaml
# Kubernetes
spec:
  containers:
  - name: api
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
      runAsNonRoot: true
      runAsUser: 10001
      seccompProfile:
        type: RuntimeDefault
```

### Container Image Scanning

**Trivy**:

```bash
# Scan local image
trivy image ghcr.io/subculture-collective/vod-tender:latest

# Scan and fail on HIGH/CRITICAL
trivy image --severity HIGH,CRITICAL --exit-code 1 vod-tender:latest
```

**Clair**:

```bash
# Using clairctl
clairctl analyze vod-tender:latest
```

**Integrate in CI/CD** (see `.github/workflows/ci.yml`):

```yaml
- name: Scan container image
  uses: aquasecurity/trivy-action@master
  with:
    image-ref: vod-tender:${{ github.sha }}
    severity: 'CRITICAL,HIGH'
    exit-code: '1'
```

### Signed Images

Use **Sigstore/Cosign** to sign and verify images:

```bash
# Sign image
cosign sign ghcr.io/subculture-collective/vod-tender:v1.0.0

# Verify signature
cosign verify ghcr.io/subculture-collective/vod-tender:v1.0.0
```

## Monitoring and Auditing

### Structured Logging

```bash
# Enable JSON logging for machine parsing
LOG_FORMAT=json
LOG_LEVEL=info
```

**Log Events**:
- Authentication attempts (success/failure)
- Authorization failures
- OAuth token refresh
- Configuration changes
- Admin actions
- Download/upload operations
- Database errors

**Log Fields**:
```json
{
  "timestamp": "2025-10-20T00:46:10Z",
  "level": "info",
  "component": "vod_download",
  "event": "download_started",
  "vod_id": "12345678",
  "correlation_id": "abc123",
  "user_id": "streamer_name",
  "ip_address": "203.0.113.10",
  "duration_ms": 1234
}
```

### Security Event Monitoring

**Alerts to Configure**:

- Multiple failed authentication attempts (potential brute force)
- Unusual number of API requests from single IP
- Database connection errors
- Circuit breaker opened (indicates systemic issues)
- Disk space critically low
- SSL certificate expiring soon (< 30 days)
- High error rate (> 5% of requests)

**Prometheus Alert Rules**:

```yaml
groups:
- name: security_alerts
  rules:
  - alert: HighErrorRate
    expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.05
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High error rate detected"
      
  - alert: UnauthorizedAccessAttempts
    expr: rate(http_requests_total{status="401"}[5m]) > 10
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "Multiple unauthorized access attempts"
      
  - alert: CircuitBreakerOpen
    expr: vod_circuit_open == 1
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "Circuit breaker is open"
```

### Audit Trail

Enable audit logging for compliance:

```bash
# PostgreSQL audit extension
# Install pgaudit
CREATE EXTENSION pgaudit;

# Configure auditing
ALTER SYSTEM SET pgaudit.log = 'write';
ALTER SYSTEM SET pgaudit.log_relation = on;
SELECT pg_reload_conf();
```

Audit logs should be:
- Tamper-proof (write-once storage or blockchain)
- Retained for compliance period (typically 1-7 years)
- Regularly reviewed for suspicious activity
- Backed up separately from operational data

## Security Benchmarks

### CIS Docker Benchmark

Key recommendations:

1. **Image Security**
   - Create user for container (non-root)
   - Use trusted base images
   - Scan images for vulnerabilities
   - Sign and verify images

2. **Container Runtime**
   - Limit container capabilities
   - Set memory and CPU limits
   - Mount root filesystem as read-only
   - Use security options (AppArmor, SELinux)

3. **Host Configuration**
   - Keep Docker up to date
   - Configure TLS authentication
   - Restrict network traffic
   - Enable content trust

**Automated Scanning**:

```bash
# docker-bench-security
git clone https://github.com/docker/docker-bench-security.git
cd docker-bench-security
sudo sh docker-bench-security.sh
```

### OWASP Top 10 Mitigation

| Vulnerability | Mitigation |
|---------------|------------|
| A01: Broken Access Control | Admin endpoints require authentication; least privilege DB access |
| A02: Cryptographic Failures | TLS for all connections; OAuth tokens encrypted at rest |
| A03: Injection | Parameterized queries; input validation |
| A04: Insecure Design | Threat modeling; security reviews |
| A05: Security Misconfiguration | Security hardening checklist; automated scanning |
| A06: Vulnerable Components | Dependabot; regular updates; CVE monitoring |
| A07: Identification/Auth Failures | Rate limiting; strong passwords; token expiry |
| A08: Software/Data Integrity | Signed container images; integrity checks |
| A09: Security Logging Failures | Structured logging; SIEM integration |
| A10: SSRF | Input validation; network egress restrictions |

### NIST Cybersecurity Framework Mapping

| Function | Category | Control |
|----------|----------|---------|
| Identify | Asset Management | Document all infrastructure components |
| Identify | Risk Assessment | Annual security assessment |
| Protect | Access Control | MFA for admin access; RBAC |
| Protect | Data Security | Encryption at rest and in transit |
| Detect | Anomalies and Events | SIEM with correlation rules |
| Detect | Continuous Monitoring | Real-time metrics and alerting |
| Respond | Incident Response | Documented runbooks; on-call rotation |
| Respond | Communications | Security advisory process |
| Recover | Recovery Planning | Tested backup and restore procedures |
| Recover | Improvements | Post-incident reviews; lessons learned |

## Security Testing

### Manual Testing

```bash
# Test TLS configuration
nmap --script ssl-enum-ciphers -p 443 vod-api.example.com

# Test for common vulnerabilities
nikto -h https://vod-api.example.com

# Fuzz API endpoints
ffuf -w wordlist.txt -u https://vod-api.example.com/api/FUZZ

# Test authentication
curl -X POST https://vod-api.example.com/admin/test
# Should return 401 Unauthorized
```

### Automated Testing

**OWASP ZAP**:

```bash
# Run baseline scan
docker run -t owasp/zap2docker-stable zap-baseline.py \
  -t https://vod-api.example.com \
  -r zap_report.html
```

**Burp Suite** (for manual testing):
- Proxy requests through Burp
- Test for SQLi, XSS, CSRF
- Analyze authentication flows
- Check for sensitive data exposure

## Compliance Checklists

### SOC 2 Type II

- [ ] Access controls documented and enforced
- [ ] Encryption in transit and at rest
- [ ] Change management process
- [ ] Incident response plan
- [ ] Vendor risk assessment
- [ ] Annual penetration test
- [ ] Security awareness training

### PCI DSS (if processing payment data)

- [ ] Network segmentation
- [ ] Strong access controls
- [ ] Encrypted cardholder data
- [ ] Regular vulnerability scans
- [ ] Intrusion detection system
- [ ] Audit logs retained 1+ year

### HIPAA (if processing health data)

- [ ] Data encryption (AES-256)
- [ ] Access controls and logging
- [ ] Business Associate Agreements
- [ ] Risk analysis conducted
- [ ] Disaster recovery plan
- [ ] Workforce training

## Additional Resources

- [OWASP Cheat Sheet Series](https://cheatsheetseries.owasp.org/)
- [CIS Benchmarks](https://www.cisecurity.org/cis-benchmarks/)
- [NIST Cybersecurity Framework](https://www.nist.gov/cyberframework)
- [Docker Security Best Practices](https://docs.docker.com/develop/security-best-practices/)
- [Kubernetes Security Best Practices](https://kubernetes.io/docs/concepts/security/security-best-practices/)

---

**Last Updated**: 2025-10-20  
**Review Schedule**: Quarterly  
**Next Review**: 2026-01-20
