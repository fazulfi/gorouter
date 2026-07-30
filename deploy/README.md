# gorouter deployment

## Quick start (target host)

```bash
# 1. Build binary locally
go build -o gorouter ./cmd/gorouter

# 2. Copy deploy/ to target (first time only)
rsync -a deploy/ gorouter@host:~/deploy/

# 3. SSH into target and run
ssh gorouter@host
cd ~/deploy

# 4. Install sudoers (first time only)
sudo cp sudoers/gorouter /etc/sudoers.d/gorouter
sudo chmod 440 /etc/sudoers.d/gorouter

# 5. Run deploy
./deploy.sh
```

## Prerequisites

- PostgreSQL accessible via `DATABASE_URL` or `PGHOST`/`PGPORT`/`PGUSER`/`PGPASSWORD`
- gorouter systemd unit installed at `/etc/systemd/system/gorouter.service`
- gorouter user has sudo for `systemctl` and `journalctl` (see `sudoers/gorouter`)
- Go 1.23+ for building

## Files

| Path | Purpose |
|------|---------|
| `deploy.sh` | Main deployment script — PG check, binary update, migrations, service restart |
| `sudoers/gorouter` | sudoers drop-in restricting gorouter to systemctl + journalctl |
