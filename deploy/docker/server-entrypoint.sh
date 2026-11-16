#!/bin/sh
# server-entrypoint.sh - pick the server role for this container.
#
#   agent | gameworld | upgrade | provision | init | hash-password
#
# provision and upgrade are added by the compose task; every role execs, so
# the binary is PID 1 and receives docker's SIGTERM.
set -eu

role="${1:-}"
[ "$#" -gt 0 ] && shift

case "$role" in
    agent) exec sro-agent "$@" ;;
    gameworld) exec sro-gameworld "$@" ;;
    init) exec sro-init "$@" ;;
    hash-password) exec hash-agent-password "$@" ;;
    *)
        echo "server-entrypoint: unknown role '$role' (agent, gameworld, upgrade, provision, init, hash-password)" >&2
        exit 2 ;;
esac
