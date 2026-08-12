# Troubleshooting

1. Check `GET /health`.
2. Inspect service logs for redacted errors.
3. Confirm `DATABASE_URL` and provider reachability in the deployment environment.
4. If a release fails health checks, follow the [deployment rollback procedure](deployment-checklist.md#rollback-procedure-if-deployment-fails).

Never paste credentials or tokens into issue reports.