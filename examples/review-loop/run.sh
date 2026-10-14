#!/bin/sh
set -eu
umask 077

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
bin_dir=${KAIROS_BIN_DIR:-$repository_root/bin}
if [ -x "$repository_root/kairos-server" ]; then bin_dir=${KAIROS_BIN_DIR:-$repository_root}; fi
listen_addr=${KAIROS_REVIEW_LOOP_ADDR:-127.0.0.1:18083}
case "$listen_addr" in 127.0.0.1:*) ;; *) printf 'Example must bind to 127.0.0.1.\n' >&2; exit 1;; esac
base_url="http://$listen_addr"
for dependency in curl jq openssl perl; do command -v "$dependency" >/dev/null || exit 1; done
test -x "$bin_dir/kairos-server"
test -x "$bin_dir/kairos-daemon"
if [ "${KAIROS_REVIEW_LOOP_SMOKE:-}" != 1 ]; then
  : "${KAIROS_DAEMON_CODEX_HOME:?Set a dedicated authenticated Codex home}"
  : "${KAIROS_DAEMON_MODEL:?Choose a model explicitly; this example uses model quota}"
fi
if curl --max-time 2 --fail --silent "$base_url/healthz" >/dev/null 2>&1; then
  printf 'Address already in use: %s\n' "$base_url" >&2; exit 1
fi
demo_dir=$(mktemp -d "${TMPDIR:-/tmp}/kairos-review-loop.XXXXXX")
server_pid=""
writer_pid=""
checker_pid=""
cleanup() {
  # Repeated terminal signals must not interrupt the ordered waits below.
  trap '' INT TERM
  for role_pid in "$writer_pid" "$checker_pid"; do
    if [ -n "$role_pid" ]; then kill "$role_pid" 2>/dev/null || true; fi
  done
  for role_pid in "$writer_pid" "$checker_pid"; do
    if [ -n "$role_pid" ]; then wait "$role_pid" 2>/dev/null || true; fi
  done
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
operator_token=$(request "$admin_token" /api/v1/identities '{"kind":"human","id":"review-loop-operator"}' | jq -er '.data.token')
writer_token=$(request "$admin_token" /api/v1/identities '{"kind":"agent","id":"review-loop-writer","role":"writer"}' | jq -er '.data.token')
checker_token=$(request "$admin_token" /api/v1/identities '{"kind":"agent","id":"review-loop-checker","role":"checker"}' | jq -er '.data.token')
request "$operator_token" /api/v1/definitions/workflows/review-loop/versions "@$script_dir/workflow.json" >/dev/null
work_item_payload=${KAIROS_REVIEW_LOOP_WORK_ITEM:-$script_dir/work-item.json}
work_id=$(request "$operator_token" /api/v1/work-items "@$work_item_payload" | jq -er '.data.id')
# The operator credential stays in a private file for console sign-in; never print it.
printf '%s' "$operator_token" > "$demo_dir/operator.token"
printf '%s' "$work_id" > "$demo_dir/work-item.id"
printf 'WorkItem: %s/work-items/%s\nOperator Token file: %s/operator.token\n' "$base_url" "$work_id" "$demo_dir"
unset admin_token operator_token
if [ "${KAIROS_REVIEW_LOOP_SMOKE:-}" = 1 ]; then
  printf 'Review-loop fixtures created successfully; no Harness/model started.\n'; exit 0
fi
KAIROS_DAEMON_TOKEN="$writer_token" "$bin_dir/kairos-daemon" --adapter codex \
  --core-url "$base_url" --codex-home "$KAIROS_DAEMON_CODEX_HOME" --codex-model "$KAIROS_DAEMON_MODEL" \
  --workspace-root "$demo_dir/writer-workspaces" --slots 1 --max-attempts 1 --max-dispatches 1 >"$demo_dir/writer.log" 2>&1 &
writer_pid=$!
KAIROS_DAEMON_TOKEN="$checker_token" "$bin_dir/kairos-daemon" --adapter codex \
  --core-url "$base_url" --codex-home "$KAIROS_DAEMON_CODEX_HOME" --codex-model "$KAIROS_DAEMON_MODEL" \
  --workspace-root "$demo_dir/checker-workspaces" --slots 1 --max-attempts 1 --max-dispatches 1 >"$demo_dir/checker.log" 2>&1 &
checker_pid=$!
unset writer_token checker_token
printf 'Writer/Checker running. Use the console for Human Review and Continue; Ctrl-C stops both Daemons, then Core.\n'
# If either role exits unexpectedly, do not leave the other role running unattended.
while kill -0 "$writer_pid" 2>/dev/null && kill -0 "$checker_pid" 2>/dev/null; do sleep 1; done
printf 'A role exited unexpectedly; inspect the private writer/checker logs.\n' >&2
exit 1
