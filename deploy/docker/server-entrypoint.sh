#!/bin/sh
# server-entrypoint.sh - pick the server role for this container.
#
#   agent | gameworld | upgrade | provision | init | hash-password
#
# Every process that opens the shard authority runs as PID 1 (exec): the
# authority lock stores its owner's pid and refuses a pid that looks alive,
# and pids repeat across containers. As PID 1, a new container's opener has
# the stored pid itself, which the lock treats as its own. Long-running
# roles exec for docker's SIGTERM too.
set -eu

role="${1:-}"
[ "$#" -gt 0 ] && shift

case "$role" in
    agent) exec sro-agent "$@" ;;
    gameworld) exec sro-gameworld "$@" ;;
    init) exec sro-init "$@" ;;
    hash-password) exec hash-agent-password "$@" ;;
    provision)
        # Creates what is missing and never replaces what exists.
        sro-provision-identity -state-dir /cluster
        if [ ! -d /data/shard/authority ]; then
            # Not exec'd: the operator token below needs the directory. The pid it
            # leaves in the lock is harmless: the lock keeps its write instant, and a
            # process born after it is never taken for the owner (store/lockfile.go).
            sro-init -shard "${SRO_SHARD_ID:?SRO_SHARD_ID is required}" -authority-dir /data/shard/authority
        fi
        # The GameWorld turns its operator API on only when this file exists
        # (player_operations.go); the admin panel holds the same token.
        umask 077
        token=/data/shard/authority/operator-token
        if [ ! -s "$token" ]; then
            head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > "$token.tmp"
            mv "$token.tmp" "$token"
        fi
        # The panel's copies, one operator token per shard: it mounts this
        # volume alone, never the cluster secrets (the session key ring) or a
        # shard's database.
        if [ -d /admin ]; then
            cp "$token" "/admin/operator-token-$SRO_SHARD_ID"
            cp /cluster/agent-provisioning-token /admin/agent-provisioning-token
        fi
        ;;
    upgrade)
        # Commits the current schema when the authority is older and is a no-op when it is
        # current (scripts/local/preboot.mjs). rollout.sh has already backed the volume up.
        if [ ! -d /data/shard/authority ]; then
            echo "upgrade: no authority yet (provision creates it)"
            exit 0
        fi
        exec sro-authority-upgrade -authority-dir /data/shard/authority -commit
        ;;
    *)
        echo "server-entrypoint: unknown role '$role' (agent, gameworld, upgrade, provision, init, hash-password)" >&2
        exit 2 ;;
esac
