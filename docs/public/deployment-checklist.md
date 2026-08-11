# Gorouter Deployment Checklist

## Pre-Deployment (Required)
- [ ] Verify production `/opt/gorouter` backed up to remote storage
- [ ] Confirm previous known-good SHA recorded for rollback
- [ ] Test all migrations in staging environment first
- [ ] Verify health endpoint returns 200: `GET /healthz`
- [ ] Confirm database schema matches expected migration version
- [ ] Review all pending PRs merged and CI green

## Deployment Steps
1. **SSH into production VPS as non-root `gorouter` user**
   ```bash
   ssh gorouter@<production-ip>
   cd /opt/gorouter
   ```

2. **Pull latest master**
   ```bash
   GIT_MASTER=1 git fetch origin master
   GIT_MASTER=1 git checkout master
   GIT_MASTER=1 git pull origin master
   ```

3. **Build frontend (if needed)**
   ```bash
   export PATH=/usr/local/go/bin:$PATH
   cd frontend && npm install && npm run build && cd ..
   ```

4. **Run database migrations**
   ```bash
   gorouter migrate up
   ```

5. **Restart service**
   ```bash
   sudo systemctl restart gorouter.service
   sudo systemctl status gorouter.service
   ```

6. **Verify deployment**
   - Health check: `curl http://localhost:8080/healthz` should return 200
   - Frontend served at `/` with SPA fallback for client routes
   - Admin API responds: `GET /api/v1/healthz`
   - SSE streams connect successfully

## Rollback Procedure (If Deployment Fails)
<a id="rollback-procedure-if-deployment-fails"></a>
1. **Stop service**
   ```bash
   sudo systemctl stop gorouter.service
   ```

2. **Restore previous binary**
   ```bash
   cp /opt/gorouter/backup/gorouter-prev /usr/local/bin/gorouter
   ```

3. **Run database rollback if schema changed**
   ```bash
   gorouter migrate down
   ```

4. **Restart service**
   ```bash
   sudo systemctl start gorouter.service
   sudo systemctl status gorouter.service
   ```

5. **Verify all functionality restored**
   - Test login/logout flow
   - Confirm dashboard loads
   - Validate API endpoints respond

## Post-Deployment Monitoring (First 24 Hours)
- Monitor logs: `journalctl -u gorouter.service -f`
- Watch for error spikes in observability dashboard
- Verify SSE connections stable (no reconnect storms)
- Confirm Admin API auth flows working
- Check compatibility layer routes preserve historical behavior
- Monitor token usage and billing accuracy
- Validate backup job runs successfully (if scheduled)

## Security Validation
<a id="security-validation"></a>
- [ ] Verify PostgreSQL bound loopback only (`127.0.0.1:5442/5443/5444`)
- [ ] Confirm non-root user running (uid 1000, group docker)
- [ ] Test CSRF protection on mutation endpoints
- [ ] Validate HSTS headers present on HTTPS
- [ ] Confirm CSP nonce-based script execution works
- [ ] Verify no secrets leaked in logs or error messages
- [ ] Test OIDC flow end-to-end with real provider

## Emergency Contacts
If deployment fails and rollback does not restore service:
- Platform Engineering Team
- Database Administrator (for schema issues)
- Security Team (for auth/OIDC issues)
