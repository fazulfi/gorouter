# Backups and restore

Schedule PostgreSQL backups and verify each backup before relying on it. Retention keeps the configured number of recent backups; the release default is 30.

Download backups through the authenticated admin API. Destructive restore operations are local-CLI-only and require typed confirmation. Run a restore drill before production rollout.