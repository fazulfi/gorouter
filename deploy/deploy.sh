#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# deploy.sh — gorouter production deployment script
#
# Prerequisites:
#   - Go binary already built (go build -o gorouter ./cmd/gorouter)
#   - PostgreSQL accessible via $DATABASE_URL or $PGHOST/PGPORT/PGUSER/PGPASSWORD
#   - gorouter systemd service already installed (see deploy/README.md)
#   - User has sudo on target host (passwordless for systemctl/journalctl)
#
# Usage:
#   ./deploy/deploy.sh [--dry-run]
# ============================================================

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=true
fi

info()  { echo "[INFO]  $*"; }
warn()  { echo "[WARN]  $*"; }
error() { echo "[ERROR] $*"; exit 1; }

run() {
  if $DRY_RUN; then
    echo "[DRY-RUN] $*"
  else
    "$@"
  fi
}

# ---- paths --------------------------------------------------
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="gorouter"
BINARY_SRC="./gorouter"
BINARY_DST="${INSTALL_DIR}/${BINARY_NAME}"
SYSTEMD_UNIT="gorouter.service"
MIGRATION_CMD="${BINARY_DST} migrate"

# ---- 1. PostgreSQL connectivity check -----------------------
info "Checking PostgreSQL connectivity..."
if ! command -v psql &>/dev/null; then
  error "psql not found — install postgresql-client or run on the DB host"
fi

if [[ -n "${DATABASE_URL:-}" ]]; then
  psql_conn_str="$DATABASE_URL"
else
  psql_conn_str="postgres://${PGUSER:-gorouter}:${PGPASSWORD:-}@${PGHOST:-localhost}:${PGPORT:-5432}/${PGDATABASE:-gorouter}?sslmode=${PGSSLMODE:-require}"
fi

if psql "$psql_conn_str" -c "SELECT 1;" &>/dev/null; then
  info "PostgreSQL connection OK"
else
  error "Cannot connect to PostgreSQL — check DATABASE_URL or PG* env vars"
fi

# ---- 2. Binary update ---------------------------------------
info "Deploying ${BINARY_NAME} binary..."

if [[ ! -f "$BINARY_SRC" ]]; then
  error "Binary not found at ${BINARY_SRC} — run 'go build -o gorouter ./cmd/gorouter' first"
fi

run sudo cp "$BINARY_SRC" "$BINARY_DST"
run sudo chmod 755 "$BINARY_DST"

installed_version=$("$BINARY_DST" version 2>/dev/null || echo "unknown")
info "Installed ${BINARY_NAME} version: ${installed_version}"

# ---- 3. Run database migrations -----------------------------
info "Running database migrations..."
run sudo -u gorouter "$MIGRATION_CMD"
info "Migrations complete"

# ---- 4. Restart service -------------------------------------
info "Restarting ${SYSTEMD_UNIT}..."
run sudo systemctl daemon-reload
run sudo systemctl restart "$SYSTEMD_UNIT"

# Give the service a moment to start
sleep 2

# ---- 5. Verify service --------------------------------------
info "Verifying service status..."
if sudo systemctl is-active --quiet "$SYSTEMD_UNIT"; then
  info "${SYSTEMD_UNIT} is active (running)"
else
  warn "${SYSTEMD_UNIT} is not active — checking logs..."
  sudo journalctl -u "$SYSTEMD_UNIT" --no-pager -n 30 2>/dev/null || true
  error "${SYSTEMD_UNIT} failed to start — see journalctl output above"
fi

info "Deployment complete!"
