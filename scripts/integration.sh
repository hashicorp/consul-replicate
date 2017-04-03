#!/bin/bash
set -e

LOG_LEVEL="ERR"

DATADIR_DC1=$(mktemp -d /tmp/consul-test1.XXXXXXXXXX)
DATADIR_DC2=$(mktemp -d /tmp/consul-test2.XXXXXXXXXX)

BIND="127.0.0.1"
PORT_DC1="8100"
PORT_DC2="8200"
ADDRESS_DC1="127.0.0.1:$PORT_DC1"
ADDRESS_DC2="127.0.0.1:$PORT_DC2"
EXCLUDED_KEY=5

function cleanup {
  kill -9 $CONSUL_DC1_PID  &> /dev/null
  kill -9 $CONSUL_DC2_PID  &> /dev/null
  kill -9 $CONSUL_REPLICATE_PID  &> /dev/null
}
trap cleanup EXIT

echo "--> Printing test information..."
echo
echo "  DATADIR_DC1: $DATADIR_DC1"
echo "  DATADIR_DC2: $DATADIR_DC2"
echo "  ADDRESS_DC1: $ADDRESS_DC1"
echo "  ADDRESS_DC2: $ADDRESS_DC2"
echo

echo "--> Building Consul Replicate..."
CONSUL_REPLICATE_BIN=$(mktemp /tmp/consul-replicate.XXXXXXXXXX)
go build -o $CONSUL_REPLICATE_BIN

echo "--> Starting Consul in DC1..."
echo "{\"ports\": {\"http\": $PORT_DC1, \"dns\": 8101, \"rpc\": 8102, \"serf_lan\": 8103, \"serf_wan\": 8104, \"server\": 8105}}" > $DATADIR_DC1/config
consul agent \
  -server \
  -bootstrap \
  -datacenter dc1 \
  -bind $BIND \
  -config-file $DATADIR_DC1/config \
  -log-level=err \
  -data-dir $DATADIR_DC1 &> /dev/null &
CONSUL_DC1_PID=$!
sleep 3

echo "--> Starting Consul in DC2..."
echo "{\"ports\": {\"http\": $PORT_DC2, \"dns\": 8201, \"rpc\": 8202, \"serf_lan\": 8203, \"serf_wan\": 8204, \"server\": 8205}}" > $DATADIR_DC2/config
consul agent \
  -server \
  -bootstrap \
  -datacenter dc2 \
  -join-wan 127.0.0.1:8104 \
  -bind $BIND \
  -config-file $DATADIR_DC2/config \
  -log-level=err \
  -data-dir $DATADIR_DC2 &> /dev/null &
CONSUL_DC2_PID=$!
sleep 3

echo "--> Creating keys in DC1..."
for i in `seq 1 10`; do
  consul kv put -http-addr="$ADDRESS_DC1" "global/$i" "test data" > /dev/null
done
consul kv put -http-addr="$ADDRESS_DC1" "globalization" "test data" > /dev/null

echo "--> Starting consul-replicate with -once..."
$CONSUL_REPLICATE_BIN \
  -consul $ADDRESS_DC2 \
  -prefix "global@dc1:backup" \
  -exclude "global/$EXCLUDED_KEY" \
  -log-level $LOG_LEVEL \
  -once

echo "--> Checking for DC2 replication..."
for i in `seq 1 10`; do
  printf "    backup/$i... "
  if [ $i -ne "$EXCLUDED_KEY" ]; then
    consul kv get -http-addr="$ADDRESS_DC2" "backup/$i" | grep -q "test data"
  else
    consul kv get -http-addr="$ADDRESS_DC2" "backup/$i" | grep -q "Error!"
  fi
  echo "OK!"
done
consul kv get -http-addr="$ADDRESS_DC2" "backupization" | grep -q "test data"

echo "--> Starting consul-replicate as a service..."
$CONSUL_REPLICATE_BIN \
  -consul $ADDRESS_DC2 \
  -prefix "global@dc1:backup" \
  -exclude "global/$EXCLUDED_KEY" \
  -excludematch "excluded_" \
  -log-level $LOG_LEVEL &
CONSUL_REPLICATE_PID=$!
sleep 3

echo "--> Checking for live replication..."
curl -sLo /dev/null -X PUT $ADDRESS_DC1/v1/kv/global/six -d "six"
sleep 3
curl -sL $ADDRESS_DC2/v1/kv/backup/six | grep -q "c2l4"

echo "##Test Case #1"
echo "    Writing a key in DC2"
curl -sLo /dev/null -X PUT $ADDRESS_DC2/v1/kv/backup/$EXCLUDED_KEY/nodelete -d "don't delete"
sleep 3

echo "    Updating prefix in DC1"
curl -sLo /dev/null -X PUT $ADDRESS_DC1/v1/kv/global/$EXCLUDED_KEY -d "test data"
sleep 3

echo "    Checking key still exists in DC2"
curl -sL $ADDRESS_DC2/v1/kv/backup/$EXCLUDED_KEY/nodelete | grep -q "ZG9uJ3QgZGVsZXRl"

echo "##Test Case #2"
echo "    Writing a key in DC2"
curl -sLo /dev/null -X PUT $ADDRESS_DC2/v1/kv/backup/excluded_key -d "don't delete"
sleep 3

echo "    Updating prefix in DC1"
curl -sLo /dev/null -X PUT $ADDRESS_DC1/v1/kv/global/parent_folder/other_folder/anykey -d "test data"
sleep 3

echo "    Checking key still exists in DC2"
curl -sL $ADDRESS_DC2/v1/kv/backup/excluded_key | grep -q "ZG9uJ3QgZGVsZXRl"

rm -rf $DATADIR_DC1
rm -rf $DATADIR_DC2

echo "--> Done!"
