#!/usr/bin/env bash
set -euo pipefail

# Simple data generator to emulate a "Producer" writing JSON swap events into Kafka/Redpanda.
# Prefers `rpk`. Falls back to `kcat` (aka kafkacat) if available.
#
# Usage:
#   ./infra/kafka/rolling_data.sh --brokers host:port --topic raw-swaps [--rps 200] [--duration 30] [--chain 1]
#
# Notes:
# - Generates valid JSON lines similar to your schema (simplified).
# - Key = "chain:tx_hash:log_index" for partitioning stability.
# - For high RPS, rpk/kcat is more efficient when fed via a pipe; this script prints per-message.

# Defaults
brokers="localhost:9092"
topic="raw-swaps"
rps=100            # messages per second
duration=30        # seconds
chain_id=1
token_list=("USDC" "USDT" "DAI" "ETH" "WBTC" "ARB" "OP" "SOL")
pool_list=("0xPoolA" "0xPoolB" "0xPoolC" "0xPoolD")
side_list=("buy" "sell")

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    --brokers)  brokers="$2"; shift 2 ;;
    --topic)    topic="$2"; shift 2 ;;
    --rps)      rps="$2"; shift 2 ;;
    --duration) duration="$2"; shift 2 ;;
    --chain)    chain_id="$2"; shift 2 ;;
    -h|--help)
      echo "Usage: $0 --brokers host:port --topic raw-swaps [--rps 200] [--duration 30] [--chain 1]"
      exit 0 ;;
    *)
      echo "Unknown flag: $1"; exit 1 ;;
  esac
done

if [[ -z "${topic}" || -z "${brokers}" ]]; then
  echo "ERROR: --brokers and --topic are required"
  exit 1
fi

have_rpk=false
have_kcat=false
if command -v rpk >/dev/null 2>&1; then have_rpk=true; fi
if command -v kcat >/dev/null 2>&1; then have_kcat=true; fi

if [[ "${have_rpk}" = false && "${have_kcat}" = false ]]; then
  echo "ERROR: Need 'rpk' or 'kcat' installed."
  exit 1
fi

echo "==> Producing to '${topic}' at ~${rps} msg/s for ${duration}s (brokers=${brokers})"

end_ts=$(( $(date +%s) + duration ))

# util random helpers
rand_int() { shuf -i "$1"-"$2" -n 1; }
rand_from_array() {
  local -n arr=$1
  local idx
  idx=$(rand_int 0 $(( ${#arr[@]} - 1 )))
  echo "${arr[$idx]}"
}

produce_one() {
  local tsms
  tsms=$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)

  local tx rand_log_index token pool side amount_token amount_usd block_number removed eid key

  tx="0x$(openssl rand -hex 32 2>/dev/null || hexdump -vn16 -e '16/1 "%02x"' /dev/urandom)"
  rand_log_index=$(rand_int 0 5)
  token=$(rand_from_array token_list)
  pool=$(rand_from_array pool_list)
  side=$(rand_from_array side_list)

  # amounts
  amount_token=$(( $(rand_int 1 100000) )) # integer basis
  amount_usd=$(( $(rand_int 10 1000000) )) # cents basis

  block_number=$(rand_int 10000000 99999999)
  removed=$(rand_int 0 20) # ~5% removed (if 0..1 -> 10%, tweak as needed)
  if (( removed > 1 )); then removed=0; fi

  eid="${chain_id}:${tx}:${rand_log_index}"
  key="${eid}"

  # JSON payload
  json=$(cat <<EOF
{
  "chain_id": ${chain_id},
  "tx_hash": "${tx}",
  "log_index": ${rand_log_index},
  "event_id": "${eid}",
  "token_address": "0x$(openssl rand -hex 20 2>/dev/null || hexdump -vn20 -e '20/1 "%02x"' /dev/urandom)",
  "token_symbol": "${token}",
  "pool_address": "${pool}",
  "side": "${side}",
  "amount_token": "${amount_token}.000000000000000000",
  "amount_usd": "$((amount_usd/100)).$((amount_usd%100))",
  "event_time": "${tsms}",
  "block_number": ${block_number},
  "removed": ${removed},
  "schema_version": 1
}
EOF
)

  if [[ "${have_rpk}" = true ]]; then
    printf '%s\n' "${json}" | rpk topic produce "${topic}" -b "${brokers}" -k "${key}" >/dev/null
  else
    printf '%s\n' "${json}" | kcat -P -b "${brokers}" -t "${topic}" -k "${key}" >/dev/null
  fi
}

# Main loop with crude token bucket pacing
per_sleep=$(awk "BEGIN { printf \"%.6f\", 1/${rps} }")
while [[ $(date +%s) -lt ${end_ts} ]]; do
  for ((i=0; i<rps; i++)); do
    produce_one || true
    # tiny sleep to spread across the second
    sleep "${per_sleep}"
  done
done

echo "Done."
