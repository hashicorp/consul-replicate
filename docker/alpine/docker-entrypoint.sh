#!/bin/dumb-init /bin/sh
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

set -e

# Note above that we run dumb-init as PID 1 in order to reap zombie processes
# as well as forward signals to all processes in its session. Normally, sh
# wouldn't do either of these functions so we'd leak zombies as well as do
# unclean termination of all our sub-processes.

# CONSUL_REPLICATE_DATA_DIR is exposed as a volume for possible persistent
# storage. CONSUL_REPLICATE_CONFIG_DIR isn't exposed as a volume but you can
# compose additional config files in there if you use this image as a base, or
# use CONSUL_REPLICATE_LOCAL_CONFIG below.
CONSUL_REPLICATE_DATA_DIR=/consul-replicate/data
CONSUL_REPLICATE_CONFIG_DIR=/consul-replicate/config

# You can also set the CONSUL_REPLICATE_LOCAL_CONFIG environemnt variable to pass some
# configuration JSON without having to bind any volumes.
if [ -n "$CONSUL_REPLICATE_LOCAL_CONFIG" ]; then
  echo "$CONSUL_REPLICATE_LOCAL_CONFIG" > "$CONSUL_REPLICATE_CONFIG_DIR/local-config.hcl"
fi

# If the user is trying to run consul-replicate directly with some arguments, then
# pass them to consul-replicate.
if [ "${1:0:1}" = '-' ]; then
  set -- /bin/consul-replicate "$@"
fi

# If we are running Consul, make sure it executes as the proper user.
if [ "$1" = '/bin/consul-replicate' ]; then
  # If the data or config dirs are bind mounted then chown them.
  # Note: This checks for root ownership as that's the most common case.
  if [ "$(stat -c %u /consul-replicate/data)" != "$(id -u consul-replicate)" ]; then
    chown consul-replicate:consul-replicate /consul-replicate/data
  fi
  if [ "$(stat -c %u /consul-replicate/config)" != "$(id -u consul-replicate)" ]; then
    chown consul-replicate:consul-replicate /consul-replicate/config
  fi

  # Set the configuration directory
  shift
  set -- /bin/consul-replicate \
    -config="$CONSUL_REPLICATE_CONFIG_DIR" \
    "$@"

  # Run under the right user
  set -- gosu consul-replicate "$@"
fi

exec "$@"
