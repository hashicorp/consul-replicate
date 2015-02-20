Consul Replicate
================
[![Latest Version](http://img.shields.io/github/release/hashicorp/consul-replicate.svg?style=flat-square)][release]
[![Build Status](http://img.shields.io/travis/hashicorp/consul-replicate.svg?style=flat-square)][travis]

[release]: https://github.com/hashicorp/consul-replicate/releases
[travis]: http://travis-ci.org/hashicorp/consul-replicate

This project provides a convenient way to replicate K/V pairs across multiple [Consul][] datacenters using the `consul-replicate` daemon.

The daemon `consul-replicate` integrates with [Consul][] to perform cross-datacenter K/V replication. This makes it possible to manage application configuration from a central datacenter, with low-latency asyncronous replication to other datacenters, thus avoiding the need for smart clients which would need to write to all datacenters and queue writes to handle network failures.

[Consul Replicate][] uses a highly-available, pull based architecture. Multiple instances of consul-replicate can run per datacenter for redundancy and high-availablilty. They use Consul's [leader election][] to elect a single node to perform the replication and gracefully failover. The active replicator watches the remote datacenter for changes and updates the local K/V store as appropriate. The daemon should not be used to attempt master-master replication. It is not designed for this use case, and will not ever reach a stable point. Instead, it should be used only for master-slave replication, where a single datacenter is considered authoritative.

**The documentation in this README corresponds to the master branch of Consul Replicate. It may contain unreleased features or different APIs than the most recently released version. Please see the Git tag that corresponds to your version of Consul Replicate for the proper documentation.**


Installation
------------
You can download a released `consul-replicate` artifact from [the Consul Replicate release page][Releases] on GitHub. If you wish to compile from source, you will need to have buildtools and [Go][] installed:

```shell
$ git clone https://github.com/hashicorp/consul-replicate.git
$ cd consul-replicate
$ make
```

This process will create `bin/consul-replicate` which make be invoked as a binary.


Usage
-----
### Options
|       Option      | Description |
| ----------------- |------------ |
| `auth`            | The basic authentication username (and optional password), separated by a colon. There is no default value.
| `consul`*         | The location of the Consul instance to query (may be an IP address or FQDN) with port.
| `max-stale`       | The maximum staleness of a query. If specified, Consul will distribute work among all servers instead of just the leader. The default value is 0 (none).
| `ssl`             | Use HTTPS while talking to Consul. Requires the Consul server to be configured to serve secure connections. The default value is false.
| `ssl-verify`      | Verify certificates when connecting via SSL. This requires the use of `-ssl`. The default value is true.
| `syslog`          | Send log output to syslog (in addition to stdout and stderr). The default value is false.
| `syslog-facility` | The facility to use when sending to syslog. This requires the use of `-syslog`. The default value is `LOCAL0`.
| `token`           | The [Consul API token][Consul ACLs]. There is no default value.
| `prefix`*         | The source prefix including the datacenter, with optional destination prefix, separated by a colon (`:`). This option is additive and may be specified multiple times for multiple prefixes to replicate.
| `wait`            | The `minimum(:maximum)` to wait for stability before replicating, separated by a colon (`:`). If the optional maximum value is omitted, it is assumed to be 4x the required minimum value. There is no default value.
| `retry`           | The amount of time to wait if Consul returns an error when communicating with the API. The default value is 5 seconds.
| `config`          | The path to a configuration file or directory of configuration files on disk, relative to the current working directory. Values specified on the CLI take precedence over values specified in the configuration file. There is no default value.
| `log-level`       | The log level for output. This applies to the stdout/stderr logging as well as syslog logging (if enabled). Valid values are "debug", "info", "warn", and "err". The default value is "warn".
| `once`            | Run Consul Replicate once and exit (as opposed to the default behavior of daemon). _(CLI-only)_
| `version`         | Output version information and quit. _(CLI-only)_

\* = Required parameter

Additionally. the following options are available for advanced users. It is not recommended you change these values unless you have a specific use case.

|       Option      | Description |
| ----------------- |------------ |
| `lock`            | The path in the KV store that is used to perform leader election for the replicators. The default value is "service/consul-replicate/leader".
| `status`          | The path in the KV store that is used to store the replication status. The default value is "service/consul-replicate/status".
| `service`         | The name of the service that is registered in Consul's catalog. The default value is "consul-replicate".


### Command Line
The CLI interface supports all of the options detailed above.

Replicate all keys under "global" from the nyc1 datacenter:

```shell
$ consul-replicate \
  -prefix "global@nyc1"
```

Replicate all keys under "global" from the nyc1 datacenter, renaming the key to "default" in the replicated stores:

```shell
$ consul-replicate \
  -prefix "global@nyc1:default"
```

Replicate all keys under "global" from the nyc1 datacenter, but do not poll or watch for changes (just do it one time):

```shell
$ consul-replicate \
  -prefix "global@nyc1" \
  -once
```

### Configuration File(s)
The Consul Replicate configuration files are written in [HashiCorp Configuration Language (HCL)][HCL]. By proxy, this means the Consul Replicate configuration file is JSON-compatible. For more information, please see the [HCL specification][HCL].

The Configuration file syntax interface supports all of the options detailed above, unless otherwise noted in the table.

```javascript
consul = "127.0.0.1:8500"
token = "abcd1234"
retry = "10s"
max_stale = "10m"

auth {
  enabled = true
  username = "test"
  password = "test"
}

ssl {
  enabled = true
  verify = false
}

syslog {
  enabled = true
  facility = "LOCAL5"
}

prefix {
  source = "global@nyc1"
}


prefix {
  source = "global@nyc1"
  destination = "default"
}

prefix {
  // Multiple prefix definitions are supported
}
```

If a directory is given instead of a file, all files in the directory (recursively) will be merged in [lexical order](http://golang.org/pkg/path/filepath/#Walk). So if multiple files declare a "consul" key for instance, the last one will be used.

**Commands specified on the command line take precedence over those defined in a config file!**


Debugging
---------
Consul Replicate can print verbose debugging output. To set the log level for Consul Replicate, use the `-log-level` flag:

```shell
$ consul-replicate -log-level info ...
```

```text
<timestamp> [INFO] (cli) received redis from Watcher
<timestamp> [INFO] (cli) invoking Runner
# ...
```

You can also specify the level as debug:

```shell
$ consul-replicate -log-level debug ...
```

```text
TODO
# ...
```


FAQ
---
**Q: Cam I use this for master-master replication?**<br>
A: No, a proper leader will never be elected.


Contributing
------------
To hack on Consul Replicate, you will need a modern [Go][] environment. To compile the `consul-replicate` binary and run the test suite, simply execute:

```shell
$ make
```

This will compile the `consul-replicate` binary into `bin/consul-replicate` and run the test suite.

If you just want to run the tests:

```shell
$ make
```

Or to run a specific test in the suite:

```shell
go test ./... -run SomeTestFunction_name
```

Submit Pull Requests and Issues to the [Consul Replicate project on GitHub][Consul Replicate].


[Consul]: http://consul.io/ "Service discovery and configuration made easy"
[leader election]: http://www.consul.io/docs/guides/leader-election.html "Consul Leader election"
[Releases]: https://github.com/hashicorp/consul-replicate/releases "Consul Replicate releases page"
[HCL]: https://github.com/hashicorp/hcl "HashiCorp Configuration Language (HCL)"
[Go]: http://golang.org "Go the language"
[Consul ACLs]: http://www.consul.io/docs/internals/acl.html "Consul ACLs"
[Consul Replicate]: https://github.com/hashicorp/consul-replicate "Consul Replicate on GitHub"
