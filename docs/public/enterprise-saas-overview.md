# Gorouter Enterprise/SaaS Deployment Guide

## Product Overview
Gorouter is a production-ready API routing platform with Admin API, compatibility layer, dashboard frontend, and real-time monitoring capabilities.

## Features Delivered (Phase 4)
- **Admin API v1**: Complete REST API with OpenAPI 3.0 contract (119 paths/153 operations)
- **Compatibility Layer**: Historical route adapters maintaining backward compatibility
- **Dashboard Frontend**: React-based admin UI with theme persistence, i18n (English/Indonesian), accessibility compliance
- **Real-Time Monitoring**: SSE streams for usage, console, providers, jobs
- **Security**: Session/PAT auth, OIDC support, CSRF protection, HSTS, CSP nonce-based security headers
- **Embed**: Production SPA fallback serving embedded frontend dist

## Production Deployment Requirements
- Go 1.25+ runtime
- Node.js 22.23+ (frontend build)
- PostgreSQL 16/17/18 (loopback only)
- Docker for containerized services
- Non-root `gorouter` user (uid 1000)
- `/var/tmp` writable workspace

## Deployment Checklist
1. Backup production `/opt/gorouter` before deployment
2. Deploy merged master SHA via manual rsync/git clone
3. Run database migrations (`gorouter migrate up`)
4. Verify health endpoint: `GET /health`
5. Confirm frontend served at `/` with SPA fallback
6. Test Admin API authentication flow
7. Validate SSE streams connect successfully
8. Monitor logs for errors during initial traffic

## Rollback Procedure
If deployment fails:
1. Stop service: `systemctl stop gorouter.service`
2. Restore previous known-good binary from backup
3. Run database rollback if schema changed: `gorouter migrate down`
4. Restart service: `systemctl start gorouter.service`
5. Verify all functionality restored
6. Document root cause and report incident

## Security Hardening Recommendations
- Bind PostgreSQL loopback only (`127.0.0.1:5442/5443/5444`)
- Use non-root application user (`gorouter` uid 1000)
- Enable HSTS on HTTPS endpoints
- Implement CSP with nonce-based script execution
- Store credentials in environment variables, never hardcoded
- Rotate API keys regularly
- Enable audit logging for sensitive operations

## Monitoring & Observability
- Health endpoint: `GET /health` returns 200 when ready
- SSE streams: `/api/admin/v1/usage/stream`, `/api/admin/v1/console/stream`, `/api/admin/v1/providers/stream`, `/api/admin/v1/jobs/stream`
- Log aggregation: Configure log forwarding to central collection
- Metrics: Prometheus-compatible metrics at `/metrics` (if enabled)

## Support
For issues or questions, consult internal documentation or escalate to platform engineering team.
