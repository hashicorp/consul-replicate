#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

set -e

RESULTS_DIR=${1:-"/tmp"}

LOG_LEVEL="ERR"

DATADIR_DC1=$(mktemp -d ${RESULTS_DIR}/consul-test1.XXXXXXXXXX)
DATADIR_DC2=$(mktemp -d ${RESULTS_DIR}/consul-test2.XXXXXXXXXX)

CONFIG_FILE="config.json"
BIND="127.0.0.1"
PORT_DC1="8100"
PORT_DC2="8200"
ADDRESS_DC1="127.0.0.1:${PORT_DC1}"
ADDRESS_DC2="127.0.0.1:${PORT_DC2}"
EXCLUDED_KEY="5"

function cleanup {
  [[ -n "${CONSUL_DC1_PID}" && -d "/proc/${CONSUL_DC1_PID}" ]] && kill -9 "${CONSUL_DC1_PID}" &>/dev/null
  [[ -n "${CONSUL_DC2_PID}" && -d "/proc/${CONSUL_DC2_PID}" ]] && kill -9 "${CONSUL_DC2_PID}" &>/dev/null
  [[ -n "${CONSUL_REPLICATE_PID}" && -d "/proc/${CONSUL_REPLICATE_PID}" ]] && kill -9 "${CONSUL_REPLICATE_PID}" &>/dev/null
  rm -f "${CONSUL_REPLICATE_BIN}"
}
trap cleanup EXIT

echo "--> Printing test information..."
echo
echo "  DATADIR_DC1: ${DATADIR_DC1}"
echo "  DATADIR_DC2: ${DATADIR_DC2}"
echo "  ADDRESS_DC1: ${ADDRESS_DC1}"
echo "  ADDRESS_DC2: ${ADDRESS_DC2}"
echo

echo "--> Building Consul Replicate..."
CONSUL_REPLICATE_BIN=$(mktemp ${RESULTS_DIR}/consul-replicate.XXXXXXXXXX)
go build -o "${CONSUL_REPLICATE_BIN}"

minorVersion=$(consul version | head -1 | cut -d . -f 2)
grpcTLS=""
[[ minorVersion -gt 13 ]] && grpcTLS=', "grpc_tls": -1'

echo "--> Starting Consul in DC1..."
echo "{\"ports\": {\"http\": ${PORT_DC1}, \"dns\": 8101, \"serf_lan\": 8103, \"serf_wan\": 8104, \"server\": 8105, \"grpc\": -1 ${grpcTLS}}}" > "${DATADIR_DC1}/${CONFIG_FILE}"
consul agent \
  -dev \
  -datacenter "dc1" \
  -bind "${BIND}" \
  -config-file "${DATADIR_DC1}/${CONFIG_FILE}" \
  -data-dir "${DATADIR_DC1}" \
  -log-level "${LOG_LEVEL}" \
  &> ${DATADIR_DC1}/consul-agent.log \
  &
CONSUL_DC1_PID=$!

echo "--> Starting Consul in DC2..."
echo "{\"ports\": {\"http\": ${PORT_DC2}, \"dns\": 8201, \"serf_lan\": 8203, \"serf_wan\": 8204, \"server\": 8205, \"grpc\": -1 ${grpcTLS}}}" > "${DATADIR_DC2}/${CONFIG_FILE}"
consul agent \
  -dev \
  -datacenter "dc2" \
  -bind "${BIND}" \
  -retry-join-wan "${BIND}:8104" \
  -config-file "${DATADIR_DC2}/${CONFIG_FILE}" \
  -data-dir "${DATADIR_DC2}" \
  -log-level "${LOG_LEVEL}" \
  &> ${DATADIR_DC2}/consul-agent.log \
  &
CONSUL_DC2_PID=$!

# Wait for ready
until consul kv get -keys -http-addr "${ADDRESS_DC1}" &>/dev/null; do
  if [ ! -d "/proc/${CONSUL_DC1_PID}" ]; then
    echo "ERROR: Consul agent in dc1 is not running. Check the log at ${DATADIR_DC1}/consul-agent.log"
    exit 1
  fi
  sleep 0.5
done
until consul kv get -keys -http-addr "${ADDRESS_DC2}" &>/dev/null; do
  if [ ! -d "/proc/${CONSUL_DC2_PID}" ]; then
    echo "ERROR: Consul agent in dc1 is not running. Check the log at ${DATADIR_DC2}/consul-agent.log"
    exit 1
  fi
  sleep 0.5
done

echo "--> Creating keys in DC1..."
consul kv import -http-addr="${ADDRESS_DC1}" - <<< $(cat <<EOF
[
  {"key": "global/1", "value": "dGVzdCBkYXRh"},
  {"key": "global/2", "value": "dGVzdCBkYXRh"},
  {"key": "global/3", "value": "dGVzdCBkYXRh"},
  {"key": "global/4", "value": "dGVzdCBkYXRh"},
  {"key": "global/5", "value": "dGVzdCBkYXRh"},
  {"key": "global/6", "value": "dGVzdCBkYXRh"},
  {"key": "global/7", "value": "dGVzdCBkYXRh"},
  {"key": "global/8", "value": "dGVzdCBkYXRh"},
  {"key": "global/9", "value": "dGVzdCBkYXRh"},
  {"key": "globalization", "value": "dGVzdCBkYXRh"}
]
EOF
) &>/dev/null

echo "--> Starting consul-replicate with -once..."
"${CONSUL_REPLICATE_BIN}" \
  -consul-addr "${ADDRESS_DC2}" \
  -prefix "global@dc1:backup" \
  -exclude "global/${EXCLUDED_KEY}" \
  -log-level "${LOG_LEVEL}" \
  -once
sleep 3

echo "--> Checking for DC2 replication..."
for i in `seq 1 9`; do
  printf "    backup/$i... "
  if [ "$i" != "$EXCLUDED_KEY" ]; then
    consul kv get -http-addr="${ADDRESS_DC2}" "backup/$i" | grep -q "test data"
  else
    consul kv get -http-addr="${ADDRESS_DC2}" "backup/$i" 2>&1 | grep -q "Error!"
  fi
  echo "OK!"
done
consul kv get -http-addr="${ADDRESS_DC2}" "backupization" | grep -q "test data"

echo "--> Starting consul-replicate as a service..."
"${CONSUL_REPLICATE_BIN}" \
  -consul-addr ${ADDRESS_DC2} \
  -prefix "global@dc1:backup" \
  -exclude "global/${EXCLUDED_KEY}" \
  -log-level "${LOG_LEVEL}" &
CONSUL_REPLICATE_PID=$!
sleep 3

echo "--> Checking for live replication..."
consul kv put -http-addr="${ADDRESS_DC1}" global/six "six" >/dev/null
sleep 3
consul kv get -http-addr="${ADDRESS_DC2}" backup/six | grep -q "six"

echo "    Writing a key in DC2"
consul kv put -http-addr="${ADDRESS_DC2}" "backup/${EXCLUDED_KEY}/nodelete" "don't delete" >/dev/null
sleep 3

echo "    Updating prefix in DC1"
consul kv put -http-addr="${ADDRESS_DC1}" "global/${EXCLUDED_KEY}" "test data" >/dev/null
sleep 3

echo "    Checking key still exists in DC2"
consul kv get -http-addr="${ADDRESS_DC2}" "backup/${EXCLUDED_KEY}/nodelete" | grep -q "don't delete"

echo "--> PASS"
exit 0
