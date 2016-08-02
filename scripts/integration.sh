#!/bin/bash
set -e

LOG_LEVEL="debug"

DATADIR_DC1=$(mktemp -d /tmp/consul-test1.XXXXXXXXXX)
DATADIR_DC2=$(mktemp -d /tmp/consul-test2.XXXXXXXXXX)

BIND="127.0.0.1"
PORT_DC1="8100"
PORT_DC2="8200"
ADDRESS_DC1="127.0.0.1:$PORT_DC1"
ADDRESS_DC2="127.0.0.1:$PORT_DC2"

function cleanup {
    kill -9 $CONSUL_DC1_PID
    kill -9 $CONSUL_DC2_PID
    kill -9 $CONSUL_REPLICATE_PID
}
trap cleanup EXIT

echo
echo "BUILDING CONSUL REPLICATE"
CONSUL_REPLICATE_BIN=$(mktemp /tmp/consul-replicate.XXXXXXXXXX)
go build -o $CONSUL_REPLICATE_BIN

echo
echo "LOG_LEVEL: $LOG_LEVEL"
echo "DATADIR_DC1: $DATADIR_DC1"
echo "DATADIR_DC2: $DATADIR_DC2"
echo "ADDRESS_DC1: $ADDRESS_DC1"
echo "ADDRESS_DC2: $ADDRESS_DC2"

echo
echo "STARTING CONSUL IN DC1"
echo "{\"ports\": {\"http\": $PORT_DC1, \"dns\": 8101, \"rpc\": 8102, \"serf_lan\": 8103, \"serf_wan\": 8104, \"server\": 8105}}" > $DATADIR_DC1/config
consul agent \
  -server \
  -bootstrap \
  -dc dc1 \
  -bind $BIND \
  -config-file $DATADIR_DC1/config \
  -data-dir $DATADIR_DC1 &
CONSUL_DC1_PID=$!
sleep 5

echo
echo "STARTING CONSUL IN DC2"
echo "{\"ports\": {\"http\": $PORT_DC2, \"dns\": 8201, \"rpc\": 8202, \"serf_lan\": 8203, \"serf_wan\": 8204, \"server\": 8205}}" > $DATADIR_DC2/config
consul agent \
  -server \
  -bootstrap \
  -dc dc2 \
  -join-wan 127.0.0.1:8104 \
  -bind $BIND \
  -config-file $DATADIR_DC2/config \
  -data-dir $DATADIR_DC2 &
CONSUL_DC2_PID=$!
sleep 5

echo
echo "STARTING CONSUL-REPLICATE"
echo $CONSUL_REPLICATE_BIN
$CONSUL_REPLICATE_BIN \
  -consul $ADDRESS_DC2 \
  -prefix "global@dc1:backup" \
  -exclude "global/555" \
  -log-level $LOG_LEVEL &
CONSUL_REPLICATE_PID=$!
sleep 5

echo
echo "CREATING KEYS IN DC1"
for i in `seq 1 1000`;
do
    curl -s -o /dev/null -X PUT $ADDRESS_DC1/v1/kv/global/$i -d "test data"
done
sleep 5

echo
echo "CHECKING DC2 FOR REPLICATION"
for i in `seq 1 1000`;
do
    if [ $i -ne "555" ]; then
        curl -s $ADDRESS_DC2/v1/kv/backup/$i | grep "dGVzdCBkYXRh"
    else
        curl -sw '%{http_code}' $ADDRESS_DC2/v1/kv/backup/$i | grep "404"
    fi
done

echo
echo "CHECKING FOR LIVE REPLICATION"
curl -s -o /dev/null -X PUT $ADDRESS_DC1/v1/kv/global/six -d "six"
sleep 5
curl -s $ADDRESS_DC2/v1/kv/backup/six | grep "c2l4"

echo

rm -rf $DATADIR_DC1
rm -rf $DATADIR_DC2
