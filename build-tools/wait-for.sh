#!/usr/bin/env sh
# Usage:
#   ./build-tools/wait-for.sh tcp://redpanda:9092 -- echo "redpanda ready"
#   ./build-tools/wait-for.sh http://clickhouse:8123/ping -- ./infra/kafka/create_topic.sh
#
# Options:
#   --timeout <sec>  (default 60)
#   --sleep   <sec>  (default 1)

set -eu

TIMEOUT=60
SLEEP=1

err() { echo "wait-for: $*" >&2; }

# --- parse options ---
while [ $# -gt 0 ]; do
  case "$1" in
    --timeout) TIMEOUT="$2"; shift 2 ;;
    --sleep)   SLEEP="$2";   shift 2 ;;
    --) shift; break ;;
    tcp://*|http://*|https://*)
      TARGET="$1"; shift ;;
    *)
      if [ -z "${TARGET:-}" ]; then
        TARGET="$1"; shift
      else
        break
      fi ;;
  esac
done

CMD="$@"

if [ -z "${TARGET:-}" ]; then
  err "no target provided (tcp://host:port or http(s)://url)"
  exit 1
fi

scheme=$(echo "$TARGET" | awk -F:// '{print $1}')

deadline=$(( $(date +%s) + TIMEOUT ))

ok=1

if [ "$scheme" = "tcp" ]; then
  hostport=$(echo "$TARGET" | sed 's#^tcp://##')
  host=$(echo "$hostport" | awk -F: '{print $1}')
  port=$(echo "$hostport" | awk -F: '{print $2}')

  if ! command -v nc >/dev/null 2>&1; then
    err "netcat (nc) is required for tcp checks"
    exit 2
  fi

  while :; do
    if nc -z -w 1 "$host" "$port" >/dev/null 2>&1; then
      ok=0; break
    fi
    now=$(date +%s)
    [ "$now" -ge "$deadline" ] && break
    sleep "$SLEEP"
  done

elif [ "$scheme" = "http" ] || [ "$scheme" = "https" ]; then
  if ! command -v curl >/dev/null 2>&1; then
    err "curl is required for http(s) checks"
    exit 2
  fi

  while :; do
    if curl -fsS --max-time 2 -o /dev/null "$TARGET"; then
      ok=0; break
    fi
    now=$(date +%s)
    [ "$now" -ge "$deadline" ] && break
    sleep "$SLEEP"
  done
else
  err "unsupported scheme: $scheme (use tcp:// or http(s)://)"
  exit 2
fi

if [ "$ok" -ne 0 ]; then
  err "timeout waiting for $TARGET (${TIMEOUT}s)"
  exit 3
fi

if [ -n "$CMD" ]; then
  exec $CMD
fi
