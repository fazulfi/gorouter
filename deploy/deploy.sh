#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# deploy.sh — gorouter production deployment script
#
# Prerequisites:
#   - Go binary already built (go build -o gorouter ./cmd/gorouter)
#   - PostgreSQL accessible via $DATABASE_URL or $PGHOST/PGPORT/PGUSER/PGPASSWORD
#   - GOROUTER_DDL_PASSWORD sourced from the secret store (role bootstrap)
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

# ---- 1b. Bootstrap gorouter role topology -------------------
# Externally provisioned LOGIN membership topology (no SUPERUSER):
#   gorouter             runtime role (LOGIN, not superuser)
#   gorouter_ddl         migration executor (LOGIN NOSUPERUSER INHERIT,
#                        MEMBER OF gorouter so ownership transfer works
#                        without superuser)
#   gorouter_ddl_nomember  test-only negative control (LOGIN, no membership)
# Fails closed on missing secret, missing CREATEROLE capability, or
# unsupported attribute drift (existing SUPERUSER/NOLOGIN roles).
info "Bootstrapping PostgreSQL role topology..."

if [[ -z "${GOROUTER_DDL_PASSWORD:-}" ]]; then
  error "GOROUTER_DDL_PASSWORD environment variable not set — source it from the secret store before deploying"
fi

if $DRY_RUN; then
  info "[DRY-RUN] would bootstrap roles gorouter, gorouter_ddl (LOGIN NOSUPERUSER INHERIT, MEMBER OF gorouter), gorouter_ddl_nomember, and GRANT CREATE ON SCHEMA public TO gorouter_ddl"
else
  # Capability probe: the bootstrap executor must be able to create roles.
  createrole=$(psql "$psql_conn_str" -Atc "SELECT rolcreaterole FROM pg_roles WHERE rolname = current_user;" 2>/dev/null || echo "error")
  if [[ "$createrole" != "t" ]]; then
    error "Bootstrap executor lacks CREATEROLE privilege — connect as the cluster master/admin role and re-run"
  fi

  # Suppress xtrace while the password is expanded into the psql stdin so
  # GOROUTER_DDL_PASSWORD never appears in trace output; restore afterwards.
  xtrace_was_on=false
  case $- in *x*) xtrace_was_on=true ;; esac
  set +x
  ddl_password_sql=${GOROUTER_DDL_PASSWORD//\'/\'\'}
  psql "$psql_conn_str" -v ON_ERROR_STOP=1 <<EOF || error "Role bootstrap failed — fix the bootstrap executor privileges and re-run"
\echo 'Provisioning gorouter (runtime role)...'
DO \$\$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'gorouter') THEN
    CREATE ROLE gorouter LOGIN NOSUPERUSER INHERIT PASSWORD '${ddl_password_sql}';
  ELSE
    ALTER ROLE gorouter LOGIN;
  END IF;
END \$\$;
\echo 'Provisioning gorouter_ddl (migration executor)...'
DO \$\$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'gorouter_ddl') THEN
    CREATE ROLE gorouter_ddl LOGIN NOSUPERUSER INHERIT PASSWORD '${ddl_password_sql}';
  ELSE
    ALTER ROLE gorouter_ddl LOGIN PASSWORD '${ddl_password_sql}';
  END IF;
END \$\$;
\echo 'Granting gorouter membership to gorouter_ddl...'
DO \$\$ BEGIN
  IF NOT EXISTS (
    SELECT FROM pg_auth_members m JOIN pg_roles g ON g.oid = m.roleid
    WHERE g.rolname = 'gorouter'
      AND m.member = (SELECT oid FROM pg_roles WHERE rolname = 'gorouter_ddl')
  ) THEN
    GRANT gorouter TO gorouter_ddl;
  END IF;
END \$\$;
\echo 'Provisioning gorouter_ddl_nomember (fail-closed control)...'
DO \$\$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'gorouter_ddl_nomember') THEN
    CREATE ROLE gorouter_ddl_nomember LOGIN NOSUPERUSER INHERIT PASSWORD '${ddl_password_sql}';
  END IF;
END \$\$;
\echo 'Validating role attributes (fail closed on drift)...'
DO \$\$ BEGIN
  IF EXISTS (SELECT FROM pg_roles WHERE rolname IN ('gorouter','gorouter_ddl','gorouter_ddl_nomember') AND rolsuper) THEN
    RAISE EXCEPTION 'bootstrap refused: a gorouter role is SUPERUSER; revoke superuser and re-run';
  END IF;
  IF EXISTS (SELECT FROM pg_roles WHERE rolname IN ('gorouter_ddl','gorouter_ddl_nomember') AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'bootstrap refused: a DDL role is NOLOGIN; run ALTER ROLE <role> LOGIN and re-run';
  END IF;
  IF NOT EXISTS (
    SELECT FROM pg_auth_members m JOIN pg_roles g ON g.oid = m.roleid
    WHERE g.rolname = 'gorouter'
      AND m.member = (SELECT oid FROM pg_roles WHERE rolname = 'gorouter_ddl')
  ) THEN
    RAISE EXCEPTION 'bootstrap refused: gorouter_ddl is not a member of gorouter; run GRANT gorouter TO gorouter_ddl and re-run';
  END IF;
END \$\$;
\echo 'Granting schema create privilege to gorouter_ddl...'
GRANT CREATE ON SCHEMA public TO gorouter_ddl;
EOF
  if $xtrace_was_on; then set -x; fi
fi

info "Role topology bootstrapped"

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
