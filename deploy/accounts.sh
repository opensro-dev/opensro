#!/usr/bin/env bash
# accounts.sh - create login accounts on the deployed stack.
#
#   deploy/accounts.sh --bootstrap <id>     the first account, before the Agent's first start
#   deploy/accounts.sh <id>                 any later account
#
# The Agent refuses to start with no account (sro-agent accounts.go), so the
# first one is a seed file: the password is hashed on the server by the
# upstream hash-agent-password (no echo) and written to the cluster volume's
# accounts.json, which the Agent reads at every start and inserts from only
# what it lacks. Later accounts go through the Agent's provisioning API,
# which listens on the Agent's loopback only. Passwords never reach argv or
# a log: they travel on stdin.
#
# Settings: DEPLOY_HOST, DEPLOY_PATH (deploy/deploy.env). ACCOUNTS_LOCAL=1
# targets the local compose project ACCOUNTS_PROJECT instead (the smoke test).
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"
. "$DEPLOY_DIR/settings.sh"

usage() { sed -n '2,6p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }

bootstrap=0
if [ "${1:-}" = "--bootstrap" ]; then bootstrap=1; shift; fi
[ "$#" -eq 1 ] || usage
id="$1"
# Upstream accepts any printable id up to AccountIDMaxBytes (64); this script
# builds JSON and shell lines from it, so it takes the safe subset only.
if ! [[ "$id" =~ ^[A-Za-z0-9_.-]{1,64}$ ]]; then
    echo "account id '$id': use 1 to 64 of A-Z a-z 0-9 _ . -" >&2
    exit 2
fi

#
# on_stack <tty|stdin> <shell command>
#
# Runs a command beside the stack's compose file: on the server over ssh,
# or locally for ACCOUNTS_LOCAL. "tty" gives it a terminal, "stdin" pipes ours.
#
on_stack() {
    local mode="$1" command="$2"
    if [ "${ACCOUNTS_LOCAL:-}" = 1 ]; then
        : "${ACCOUNTS_PROJECT:?ACCOUNTS_LOCAL needs ACCOUNTS_PROJECT}"
        (cd "$DEPLOY_DIR" && COMPOSE="docker compose -p $ACCOUNTS_PROJECT -f docker-compose.prod.yml -f docker-compose.smoke.yml --env-file test/smoke.env" \
            bash -c "$command")
        return
    fi
    load_settings DEPLOY_HOST DEPLOY_PATH
    : "${DEPLOY_HOST:?set DEPLOY_HOST in deploy/deploy.env (cp deploy/deploy.example.env deploy/deploy.env)}"
    check_deploy_path
    local flags=(-T)
    [ "$mode" = tty ] && flags=(-t)
    # release.env names the running images; pending.env the ones a first rollout stopped at.
    ssh "${flags[@]}" "$DEPLOY_HOST" "cd '$DEPLOY_PATH' && env_files='--env-file .env.prod' && \
        for f in release.env pending.env; do if [ -f \$f ]; then env_files=\"\$env_files --env-file \$f\"; break; fi; done && \
        COMPOSE=\"docker compose -f docker-compose.prod.yml \$env_files\" bash -c '$command'"
}

#
# json_body
#
# The provisioning request for $id with the password read from stdin,
# JSON-encoded by python so any character survives.
#
json_body() {
    python3 -c 'import json, sys; print(json.dumps({"id": sys.argv[1], "password": sys.stdin.readline().rstrip("\r\n")}))' "$id"
}

#
# read_password
#
# Twice, without echo, from the terminal; one line from stdin when there is none.
#
read_password() {
    local first second
    if [ ! -t 0 ]; then
        IFS= read -r first
        printf '%s\n' "$first"
        return
    fi
    IFS= read -rs -p "Password for $id: " first; echo >&2
    IFS= read -rs -p "Confirm: " second; echo >&2
    if [ "$first" != "$second" ]; then echo "passwords do not match" >&2; return 1; fi
    printf '%s\n' "$first"
}

if [ "$bootstrap" = 1 ]; then
    [ "${ACCOUNTS_LOCAL:-}" != 1 ] || { echo "--bootstrap needs a terminal on the server; not for ACCOUNTS_LOCAL" >&2; exit 2; }
    echo "==> hash the password on the server (typed there, no echo)"
    output="$(on_stack tty '$COMPOSE run --rm -it --no-deps --entrypoint hash-agent-password provision')"
    hash="$(printf '%s\n' "$output" | tr -d '\r' | grep -E '^\$2[aby]\$' | tail -n 1)"
    [ -n "$hash" ] || { echo "no hash came back:" >&2; printf '%s\n' "$output" >&2; exit 1; }
    echo "==> write the seed account"
    printf '[{"id":"%s","passwordHash":"%s"}]\n' "$id" "$hash" \
        | on_stack stdin '$COMPOSE run --rm -T --no-deps --entrypoint sh provision -c "set -e; if [ -e /cluster/accounts.json ]; then echo accounts.json already exists >&2; exit 1; fi; umask 077; cat > /cluster/accounts.json"'
    echo "seed account '$id' written; run the rollout again to start the Agent"
    exit 0
fi

# The provisioning token goes to curl as a header file, never on its argv
# (ps would show it to every user of the host).
password="$(read_password)"
printf '%s\n' "$password" | json_body \
    | on_stack stdin '$COMPOSE exec -T agent sh -c "umask 077; headers=\$(mktemp); trap \"rm -f \$headers\" EXIT; printf \"Authorization: Bearer %s\\n\" \"\$(cat /cluster/agent-provisioning-token)\" > \$headers; curl -fsS -X POST -H @\$headers -H content-type:application/json --data-binary @- http://127.0.0.1:8789/v1/accounts"'
echo
echo "account '$id' created"
