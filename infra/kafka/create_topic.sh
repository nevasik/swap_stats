#!/usr/bin/env bash

set -euo pipefail

# Create a Kafka/Redpanda topic with sensible defaults.
# Prefers `rpk`. Falls back to Kafka CLI (kafka-topics.sh + kafka-configs.sh).
#
# Usage:
#   ./infra/kafka/create_topic.sh <topic> [--brokers host:port] [--partitions N] [--replication N] \
#     [--retention-ms MS] [--segment-ms MS] [--cleanup delete|compact|delete,compact] [--max-msg-bytes BYTES]
#
# Defaults (dev-friendly, PROD tune in CI):
#   partitions=1, replication=1, retention.ms=604800000 (7d), segment.ms=3600000 (1h),
#   cleanup=delete, max.message.bytes=1048576 (1 MiB)

topic="${1:-}"
shift || true

if [[ -z "${topic}" ]]; then
  echo "ERROR: topic name is required."
  echo "Usage: $0 <topic> [--brokers host:port] [--partitions N] [--replication N] [--retention-ms MS] [--segment-ms MS] [--cleanup POLICY] [--max-msg-bytes BYTES]"
  exit 1
fi

# Defaults
brokers="localhost:9092"
partitions=1
replication=1
retention_ms=604800000     # 7d
segment_ms=3600000         # 1h
cleanup="delete"
max_msg_bytes=1048576      # 1 MiB

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    --brokers)        brokers="$2"; shift 2 ;;
    --partitions)     partitions="$2"; shift 2 ;;
    --replication)    replication="$2"; shift 2 ;;
    --retention-ms)   retention_ms="$2"; shift 2 ;;
    --segment-ms)     segment_ms="$2"; shift 2 ;;
    --cleanup)        cleanup="$2"; shift 2 ;;
    --max-msg-bytes)  max_msg_bytes="$2"; shift 2 ;;
    -h|--help)
      echo "Usage: $0 <topic> [--brokers host:port] [--partitions N] [--replication N] [--retention-ms MS] [--segment-ms MS] [--cleanup POLICY] [--max-msg-bytes BYTES]"
      exit 0 ;;
    *)
      echo "Unknown flag: $1"; exit 1 ;;
  esac
done

echo "==> Creating topic '${topic}' on brokers '${brokers}'"
echo "    partitions=${partitions}, replication=${replication}, retention.ms=${retention_ms}, segment.ms=${segment_ms}, cleanup.policy=${cleanup}, max.message.bytes=${max_msg_bytes}"

if command -v rpk >/dev/null 2>&1; then
  # Redpanda / Kafka via rpk
  set +e
  rpk topic create "${topic}" -b "${brokers}" -p "${partitions}" -r "${replication}" \
    --config "retention.ms=${retention_ms}" \
    --config "segment.ms=${segment_ms}" \
    --config "cleanup.policy=${cleanup}" \
    --config "max.message.bytes=${max_msg_bytes}"
  rc=$?
  set -e
  if [[ $rc -ne 0 ]]; then
    echo "Topic may already exist or rpk failed (rc=${rc}). Attempting to update config…"
    rpk topic alter-config "${topic}" -b "${brokers}" \
      --set "retention.ms=${retention_ms}" \
      --set "segment.ms=${segment_ms}" \
      --set "cleanup.policy=${cleanup}" \
      --set "max.message.bytes=${max_msg_bytes}"
  fi
  echo "Done."
  exit 0
fi

# Fallback to Apache Kafka CLI
if ! command -v kafka-topics.sh >/dev/null 2>&1; then
  echo "ERROR: neither 'rpk' nor 'kafka-topics.sh' found in PATH."
  exit 1
fi

# Create topic (ignore error if exists)
set +e
kafka-topics.sh --bootstrap-server "${brokers}" --create \
  --if-not-exists \
  --topic "${topic}" \
  --partitions "${partitions}" \
  --replication-factor "${replication}"
set -e

# Update configs
if ! command -v kafka-configs.sh >/dev/null 2>&1; then
  echo "WARN: 'kafka-configs.sh' not found. Skipping config updates."
  exit 0
fi

kafka-configs.sh --bootstrap-server "${brokers}" --alter --topic "${topic}" \
  --add-config "retention.ms=${retention_ms},segment.ms=${segment_ms},cleanup.policy=${cleanup},max.message.bytes=${max_msg_bytes}"

echo "Done."
