#!/usr/bin/env bash
# Zero-downtime rolling deploy across two instances (sis@8080, sis@8081).
#
# Usage:  sudo /opt/sis/deploy.sh sis-<sha>
# The CI step scp's the binary to /opt/sis/releases/sis-<sha>, then runs this.
set -euo pipefail

REL="${1:?usage: deploy.sh <release-tag>}"
RELEASES=/opt/sis/releases
CURRENT=/opt/sis/current
BIN="$RELEASES/$REL"
PORTS=(8080 8081)
HEALTH_RETRIES=20
HEALTH_DELAY=0.5

log() { echo "[deploy] $*"; }

# Load DATABASE_URL (and friends) so the `migrate` step below can reach Postgres.
# The instances get this via systemd's EnvironmentFile; this script needs it too.
set -a
[ -f /etc/sis/sis.env ] && . /etc/sis/sis.env
set +a

[ -x "$BIN" ] || { log "missing or non-executable binary: $BIN"; exit 1; }

# Remember the current target so we can roll back. readlink (not -f) gives the
# symlink's immediate target, not the fully-resolved path.
PREV="$(readlink "$CURRENT" 2>/dev/null || true)"

healthy() { # $1 = port — poll the instance directly, bypassing Caddy
	local port="$1" i
	for ((i = 0; i < HEALTH_RETRIES; i++)); do
		if curl -fsS "http://127.0.0.1:${port}/healthz" >/dev/null 2>&1; then
			return 0
		fi
		sleep "$HEALTH_DELAY"
	done
	return 1
}

rollback() {
	log "ROLLBACK -> ${PREV:-<none>}"
	if [ -n "$PREV" ] && [ "$PREV" != "$BIN" ]; then
		ln -sfn "$PREV" "$CURRENT"
		for p in "${PORTS[@]}"; do systemctl restart "sis@${p}" || true; done
	fi
	exit 1
}

# 1. Point current at the new release. Already-running processes keep the OLD
#    binary's inode open until they restart — that's our safety net below.
chown sis:sis "$BIN"
ln -sfn "$BIN" "$CURRENT"

# 2. Migrate ONCE, before any instance restarts. Migrations must be
#    backward-compatible: the not-yet-restarted instance still runs old code.
log "applying migrations"
"$CURRENT" migrate || rollback

# 3. Rolling restart: one instance at a time. While we cycle one, the other
#    keeps serving (Caddy's health check routes around the one that's down).
for port in "${PORTS[@]}"; do
	log "restarting sis@${port}"
	systemctl restart "sis@${port}"
	if healthy "$port"; then
		log "sis@${port} healthy"
	else
		log "sis@${port} did not come up healthy"
		rollback
	fi
done

log "deploy complete: $REL"
