#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
listen_addr=${KAIROS_QUICKSTART_ADDR:-127.0.0.1:8080}
base_url="http://$listen_addr"
demo_dir=$(mktemp -d "${TMPDIR:-/tmp}/kairos-quickstart.XXXXXX")
server_pid=""

cleanup() {
	if [ -n "$server_pid" ] && kill -0 "$server_pid" 2>/dev/null; then
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
	case "$demo_dir" in
		"${TMPDIR:-/tmp}"/kairos-quickstart.*) rm -rf -- "$demo_dir" ;;
	esac
}
trap cleanup EXIT INT TERM

KAIROS_LISTEN_ADDR="$listen_addr" \
KAIROS_SQLITE_PATH="$demo_dir/kairos.db" \
KAIROS_ARTIFACT_DIR="$demo_dir/artifacts" \
	"$repository_root/bin/kairos-server" >"$demo_dir/server.log" 2>&1 &
server_pid=$!

attempt=0
until curl --fail --silent --show-error "$base_url/healthz" >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if ! kill -0 "$server_pid" 2>/dev/null; then
		printf 'Kairos failed to start:\n' >&2
		cat "$demo_dir/server.log" >&2
		exit 1
	fi
	if [ "$attempt" -ge 50 ]; then
		printf 'Timed out waiting for Kairos at %s.\n' "$base_url" >&2
		exit 1
	fi
	sleep 0.1
done

request() {
	operation_id=$1
	endpoint=$2
	payload=$3
	curl --fail-with-body --silent --show-error \
		-X POST "$base_url$endpoint" \
		-H 'Content-Type: application/json' \
		-H 'X-Kairos-Actor-Id: quickstart-operator' \
		-H 'X-Kairos-Actor-Kind: human' \
		-H "Idempotency-Key: $operation_id" \
		--data-binary "@$payload" >/dev/null
}

request quickstart-create-definition /api/v1/definitions/workflows "$script_dir/workflow.json"
request quickstart-create-work-item /api/v1/work-items "$script_dir/work-item.json"

if [ "${KAIROS_QUICKSTART_SMOKE_TEST:-}" = "1" ]; then
	curl --fail --silent --show-error "$base_url/api/v1/work" \
		-H 'X-Kairos-Actor-Id: quickstart-verifier' \
		-H 'X-Kairos-Actor-Kind: agent' \
		-H 'X-Kairos-Actor-Role: contributor' >/dev/null
	exit 0
fi

printf '\nKairos quickstart is ready at %s\n\n' "$base_url"
printf 'The example has two parallel Tasks followed by one join Task.\n'
printf 'Open the console, then start one or more Codex sessions from this repository with:\n\n'
printf '  KAIROS_ACTOR_ID=quickstart-agent-1 KAIROS_ACTOR_KIND=agent KAIROS_ACTOR_ROLE=contributor codex\n\n'
printf 'Ask each session: Use $kairos-agent to find and complete one available Task.\n'
printf 'Press Ctrl-C here to stop Kairos and remove the temporary demo data.\n\n'

wait "$server_pid"
