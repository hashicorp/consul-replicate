# Consul Replicate

![Build Status](https://github.com/hashicorp/consul-replicate/actions/workflows/lint-and-test/badge.svg)

This project provides a convenient way to replicate values from one
[Consul][consul] datacenter to another using the `consul-replicate` daemon.

The daemon `consul-replicate` integrates with [Consul][consul] to perform
cross-data-center K/V replication. This makes it possible to manage application
configuration from a central data center, with low-latency asynchronous
replication to other data centers, thus avoiding the need for smart clients that
would need to write to all data centers and queue writes to handle network
failures.

---

**The documentation in this README corresponds to the master branch of Consul Replicate. It may contain unreleased features or different APIs than the most recently released version.**

**Please see the [Git tag](https://github.com/hashicorp/consul-replicate/releases) that corresponds to your version of Consul Replicate for the proper documentation.**

---


## Installation

1. Download a pre-compiled, released version from the [Consul Replicate releases page][releases].

1. Extract the binary using `unzip` or `tar`.

1. Move the binary into `$PATH`.

To compile from source, please see the instructions in the
[contributing section](#contributing).

## Usage

For the full list of options:

```shell
$ consul-replicate -h
```

### Command Line Flags

The CLI interface supports all options in the configuration file and visa-versa.
Here are a few examples of common integrations on the command line.

Replicate all keys under "global" from the nyc1 data center:

```sh
$ consul-replicate \
  -prefix "global@nyc1"
```

Replicate all keys under "global" from the nyc1 data center, renaming the key to
"default" in the replicated stores:

```sh
$ consul-replicate \
  -prefix "global@nyc1:default"
```

Replicate all keys under "global" from the nyc1 data center, but do not poll or
watch for changes (just do it one time):

```sh
$ consul-replicate \
  -prefix "global@nyc1" \
  -once
```

Replicate all keys under "global" from the nyc1 data center, but exclude the
global/private prefix:

```sh
$ consul-replicate \
  -prefix "global@nyc1" \
  -exclude "global/private" \
  -once
```

### Configuration File Format

Configuration files are written in the [HashiCorp Configuration Language][hcl].
By proxy, this means the configuration is also JSON compatible.

```hcl
# This denotes the start of the configuration section for Consul. All values
# contained in this section pertain to Consul.
consul {
  # This block specifies the basic authentication information to pass with the
  # request. For more information on authentication, please see the Consul
  # documentation.
  auth {
    enabled  = true
    username = "test"
    password = "test"
  }

  # This is the address of the Consul agent. By default, this is
  # 127.0.0.1:8500, which is the default bind and port for a local Consul
  # agent. It is not recommended that you communicate directly with a Consul
  # server, and instead communicate with the local Consul agent. There are many
  # reasons for this, most importantly the Consul agent is able to multiplex
  # connections to the Consul server and reduce the number of open HTTP
  # connections. Additionally, it provides a "well-known" IP address for which
  # clients can connect.
  address = "127.0.0.1:8500"

  # This is the ACL token to use when connecting to Consul. If you did not
  # enable ACLs on your Consul cluster, you do not need to set this option.
  #
  # This option is also available via the environment variable CONSUL_TOKEN.
  token = "abcd1234"

  # This controls the retry behavior when an error is returned from Consul.
  # Consul Replicate is highly fault tolerant, meaning it does not exit in the
  # face of failure. Instead, it uses exponential back-off and retry functions
  # to wait for the cluster to become available, as is customary in distributed
  # systems.
  retry {
    # This enabled retries. Retries are enabled by default, so this is
    # redundant.
    enabled = true

    # This specifies the number of attempts to make before giving up. Each
    # attempt adds the exponential backoff sleep time. Setting this to
    # zero will implement an unlimited number of retries.
    attempts = 12

    # This is the base amount of time to sleep between retry attempts. Each
    # retry sleeps for an exponent of 2 longer than this base. For 5 retries,
    # the sleep times would be: 250ms, 500ms, 1s, 2s, then 4s.
    backoff = "250ms"

    # This is the maximum amount of time to sleep between retry attempts.
    # When max_backoff is set to zero, there is no upper limit to the
    # exponential sleep between retry attempts.
    # If max_backoff is set to 10s and backoff is set to 1s, sleep times
    # would be: 1s, 2s, 4s, 8s, 10s, 10s, ...
    max_backoff = "1m"
  }

  # This block configures the SSL options for connecting to the Consul server.
  ssl {
    # This enables SSL. Specifying any option for SSL will also enable it.
    enabled = true

    # This enables SSL peer verification. The default value is "true", which
    # will check the global CA chain to make sure the given certificates are
    # valid. If you are using a self-signed certificate that you have not added
    # to the CA chain, you may want to disable SSL verification. However, please
    # understand this is a potential security vulnerability.
    verify = false

    # This is the path to the certificate to use to authenticate. If just a
    # certificate is provided, it is assumed to contain both the certificate and
    # the key to convert to an X509 certificate. If both the certificate and
    # key are specified, Consul Replicate will automatically combine them into an
    # X509 certificate for you.
    cert = "/path/to/client/cert"
    key  = "/path/to/client/key"

    # This is the path to the certificate authority to use as a CA. This is
    # useful for self-signed certificates or for organizations using their own
    # internal certificate authority.
    ca_cert = "/path/to/ca"

    # This is the path to a directory of PEM-encoded CA cert files. If both
    # `ca_cert` and `ca_path` is specified, `ca_cert` is preferred.
    ca_path = "path/to/certs/"

    # This sets the SNI server name to use for validation.
    server_name = "my-server.com"
  }
}

# This is the list of keys to exclude if they are found in the prefix. This can
# be specified multiple times to exclude multiple keys from replication.
exclude {
  source = "my-key"
}

# This is the signal to listen for to trigger a graceful stop. The default value
# is shown below. Setting this value to the empty string will cause Consul
# Replicate to not listen for any graceful stop signals.
kill_signal = "SIGINT"

# This is the log level. If you find a bug in Consul Replicate, please enable
# debug logs so we can help identify the issue. This is also available as a
# command line flag.
log_level = "warn"

# This is the maximum interval to allow "stale" data. By default, only the
# Consul leader will respond to queries; any requests to a follower will
# forward to the leader. In large clusters with many requests, this is not as
# scalable, so this option allows any follower to respond to a query, so long
# as the last-replicated data is within these bounds. Higher values result in
# less cluster load, but are more likely to have outdated data.
max_stale = "10m"

# This is the path to store a PID file which will contain the process ID of the
# Consul Replicate process. This is useful if you plan to send custom signals
# to the process.
pid_file = "/path/to/pid"

# This is the prefix and datacenter to replicate and the resulting destination.
prefix {
  source      = "global"
  datacenter  = "nyc1"
  destination = "default"
}

# This is the signal to listen for to trigger a reload event. The default value
# is shown below. Setting this value to the empty string will cause Consul
# Replicate to not listen for any reload signals.
reload_signal = "SIGHUP"

# This is the path in Consul to store replication and leader status.
status_dir = "service/consul-replicate/statuses"

# This block defines the configuration for connecting to a syslog server for
# logging.
syslog {
  # This enables syslog logging. Specifying any other option also enables
  # syslog logging.
  enabled = true

  # This is the name of the syslog facility to log to.
  facility = "LOCAL5"
}

# This is the quiescence timers; it defines the minimum and maximum amount of
# time to wait for the cluster to reach a consistent state before rendering a
# replicating. This is useful to enable in systems that have a lot of flapping,
# because it will reduce the the number of times a replication occurs.
wait {
  min = "5s"
  max = "10s"
}
```

Note that not all fields are required. If you are not logging to syslog, you do
not need to specify a syslog configuration.

For additional security, tokens may also be read from the environment using the
`CONSUL_TOKEN` environment variable. It is highly recommended that you do not
put your tokens in plain-text in a configuration file.

Instruct Consul Replicate to use a configuration file with the `-config` flag:

```shell
$ consul-replicate -config "/my/config.hcl"
```

This argument may be specified multiple times to load multiple configuration
files. The right-most configuration takes the highest precedence. If the path to
a directory is provided (as opposed to the path to a file), all of the files in
the given directory will be merged in
[lexical order](http://golang.org/pkg/path/filepath/#Walk), recursively. Please
note that symbolic links are _not_ followed.

**Commands specified on the CLI take precedence over a config file!**

## Debugging

Consul Replicate can print verbose debugging output. To set the log level for
Consul Replicate, use the `-log-level` flag:

```shell
$ consul-replicate -log-level info ...
```

```text
<timestamp> [INFO] (cli) received redis from Watcher
<timestamp> [INFO] (cli) invoking Runner
# ...
```

You can also specify the level as trace:

```shell
$ consul-replicate -log-level trace ...
```

```text
<timestamp> [DEBUG] (cli) creating Runner
<timestamp> [DEBUG] (cli) creating Consul API client
<timestamp> [DEBUG] (cli) creating Watcher
<timestamp> [DEBUG] (cli) looping for data
<timestamp> [DEBUG] (watcher) starting watch
<timestamp> [DEBUG] (watcher) all pollers have started, waiting for finish
<timestamp> [DEBUG] (redis) starting poll
<timestamp> [DEBUG] (service redis) querying Consul with &{...}
<timestamp> [DEBUG] (service redis) Consul returned 2 services
<timestamp> [DEBUG] (redis) writing data to channel
<timestamp> [DEBUG] (redis) starting poll
<timestamp> [INFO] (cli) received redis from Watcher
<timestamp> [INFO] (cli) invoking Runner
<timestamp> [DEBUG] (service redis) querying Consul with &{...}
# ...
```

## FAQ

**Q: Can I use this for master-master replication?**<br>
A: Master-master replication is not possible. A leader would never be elected.

## Contributing

To build and install Consul Replicate locally, you will need to install the
Docker engine:

- [Docker for Mac](https://docs.docker.com/engine/installation/mac/)
- [Docker for Windows](https://docs.docker.com/engine/installation/windows/)
- [Docker for Linux](https://docs.docker.com/engine/installation/linux/ubuntulinux/)

Clone the repository:

```shell
$ git clone https://github.com/hashicorp/consul-replicate.git
```

To compile the `consul-replicate` binary for your local machine:

```shell
$ make dev
```

This will compile the `consul-replicate` binary into `bin/consul-replicate` as
well as your `$GOPATH` and run the test suite.

If you want to compile a specific binary, set `XC_OS` and `XC_ARCH` or run the
following to generate all binaries:

```shell
$ make bin
```

If you just want to run the tests:

```shell
$ make test
```

Or to run a specific test in the suite:

```shell
go test ./... -run SomeTestFunction_name
```

[consul]: https://www.consul.io "Consul by HashiCorp"
[hcl]: https://github.com/hashicorp/hcl "HashiCorp Configuration Language (hcl)"
[releases]: https://releases.hashicorp.com/consul-replicate "Consul Replicate Releases"
