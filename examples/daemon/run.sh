#!/bin/sh
set -eu
umask 077

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
bin_dir=${KAIROS_BIN_DIR:-$repository_root/bin}
if [ -x "$repository_root/kairos-server" ]; then bin_dir=${KAIROS_BIN_DIR:-$repository_root}; fi
listen_addr=${KAIROS_DAEMON_EXAMPLE_ADDR:-127.0.0.1:18082}
case "$listen_addr" in 127.0.0.1:*) ;; *) printf 'Example must bind to 127.0.0.1.\n' >&2; exit 1;; esac
base_url="http://$listen_addr"
for dependency in curl jq openssl perl; do command -v "$dependency" >/dev/null || exit 1; done
test -x "$bin_dir/kairos-server"
test -x "$bin_dir/kairos-daemon"
if [ "${KAIROS_DAEMON_EXAMPLE_SMOKE:-}" != 1 ]; then
  : "${KAIROS_DAEMON_CODEX_HOME:?Set a dedicated authenticated Codex home}"
  : "${KAIROS_DAEMON_MODEL:?Choose a model explicitly; this example uses model quota}"
fi
if curl --max-time 2 --fail --silent "$base_url/healthz" >/dev/null 2>&1; then
  printf 'Address already in use: %s\n' "$base_url" >&2; exit 1
fi
demo_dir=$(mktemp -d "${TMPDIR:-/tmp}/kairos-daemon-example.XXXXXX")
server_pid=""
daemon_pid=""
cleanup() {
  # Repeated terminal signals must not interrupt the ordered waits below.
  trap '' INT TERM
  if [ -n "$daemon_pid" ]; then kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; fi
  if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
  printf 'Example stopped. Private data retained at %s; remove it when no longer needed.\n' "$demo_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

admin_token=$(openssl rand -hex 32)
# Keep Core outside the terminal's foreground process group. Perl exec preserves
# the child PID for kill/wait and works on both Linux and macOS without setsid(1).
# Explicitly clear an inherited PostgreSQL DSN so the example never targets an existing database.
KAIROS_POSTGRES_DSN="" KAIROS_AUTH_MODE=authenticated KAIROS_ADMIN_TOKEN="$admin_token" \
KAIROS_LISTEN_ADDR="$listen_addr" KAIROS_SQLITE_PATH="$demo_dir/core.db" \
KAIROS_ARTIFACT_DIR="$demo_dir/artifacts" \
  perl -MPOSIX=setpgid -e 'defined(setpgid(0, 0)) or die "setpgid: $!"; exec { $ARGV[0] } @ARGV or die "exec: $!";' \
  "$bin_dir/kairos-server" >"$demo_dir/core.log" 2>&1 &
server_pid=$!
attempt=0
until curl --max-time 2 --fail --silent "$base_url/healthz" >/dev/null 2>&1; do
  attempt=$((attempt+1))
  if ! kill -0 "$server_pid" 2>/dev/null || [ "$attempt" -ge 50 ]; then
    printf 'Core startup failed; inspect %s/core.log\n' "$demo_dir" >&2; exit 1
  fi
  sleep 0.1
done
sleep 0.1
kill -0 "$server_pid" 2>/dev/null || exit 1

# Credentials travel over stdin, never curl argv or printed shell commands.
request() {
  request_token=$1; endpoint=$2; payload=$3
  printf 'header = "Authorization: Bearer %s"\n' "$request_token" |
    curl --config - --max-time 10 --fail --silent --show-error \
      -H 'Content-Type: application/json' -H "Idempotency-Key: example-$(openssl rand -hex 16)" \
      "$base_url$endpoint" --data-binary "$payload"
}
operator_token=$(request "$admin_token" /api/v1/identities '{"kind":"human","id":"example-operator"}' | jq -er '.data.token')
agent_token=$(request "$admin_token" /api/v1/identities '{"kind":"agent","id":"example-daemon","role":"backend"}' | jq -er '.data.token')
for mode in workflow blackboard; do
  request "$operator_token" "/api/v1/definitions/${mode}s/daemon-$mode/versions" "@$script_dir/$mode.json" >/dev/null
  work_id=$(request "$operator_token" /api/v1/work-items "@$script_dir/$mode-work-item.json" | jq -er '.data.id')
  printf '%s WorkItem: %s\n' "$mode" "$work_id"
done
unset admin_token operator_token
if [ "${KAIROS_DAEMON_EXAMPLE_SMOKE:-}" = 1 ]; then
  printf 'Example fixtures created successfully; no Harness/model started.\n'; exit 0
fi
KAIROS_DAEMON_TOKEN="$agent_token" "$bin_dir/kairos-daemon" --adapter codex \
  --core-url "$base_url" --codex-home "$KAIROS_DAEMON_CODEX_HOME" --codex-model "$KAIROS_DAEMON_MODEL" \
  --workspace-root "$demo_dir/workspaces" --slots 1 --max-attempts 1 --max-dispatches 1 &
daemon_pid=$!
unset agent_token
printf 'Core: %s. Ctrl-C stops Daemon, then Core. A successful run completes both WorkItems.\n' "$base_url"
wait "$daemon_pid"
