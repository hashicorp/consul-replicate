Consul Replicate
================
[![Build Status](http://img.shields.io/travis/hashicorp/consul-replicate.svg?style=flat-square)][travis]

[travis]: https://travis-ci.org/hashicorp/consul-replicate

This project provides a convenient way to replicate K/V pairs across multiple [Consul][] data centers using the `consul-replicate` daemon.

The daemon `consul-replicate` integrates with [Consul][] to perform cross-data-center K/V replication. This makes it possible to manage application configuration from a central data center, with low-latency asynchronous replication to other data centers, thus avoiding the need for smart clients that would need to write to all data centers and queue writes to handle network failures.

**The documentation in this README corresponds to the master branch of Consul Replicate. It may contain unreleased features or different APIs than the most recently released version. Please see the Git tag that corresponds to your version of Consul Replicate for the proper documentation.**


Installation
------------
You can download a released `consul-replicate` artifact from [the Consul Replicate release page](https://releases.hashicorp.com/consul-replicate/). If you wish to compile from source, you will need to have build tools and [Go][] installed:

```shell
$ git clone https://github.com/hashicorp/consul-replicate.git
$ cd consul-replicate
$ make
```

This process will create `bin/consul-replicate` which may be invoked as a binary.


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
| `prefix`*         | The source prefix including the data center, with optional destination prefix, separated by a colon (`:`). This option is additive and may be specified multiple times for multiple prefixes to replicate.
| `exclude`         | A prefix to exclude during replication. This option is additive and may be specified multiple times for multiple prefixes to exclude.
| `wait`            | The `minimum(:maximum)` to wait for stability before replicating, separated by a colon (`:`). If the optional maximum value is omitted, it is assumed to be 4x the required minimum value. There is no default value.
| `retry`           | The amount of time to wait if Consul returns an error when communicating with the API. The default value is 5 seconds.
| `config`          | The path to a configuration file or directory of configuration files on disk, relative to the current working directory. Values specified on the CLI take precedence over values specified in the configuration file. There is no default value.
| `log-level`       | The log level for output. This applies to the stdout/stderr logging as well as syslog logging (if enabled). Valid values are "debug", "info", "warn", and "err". The default value is "warn".
| `once`            | Run Consul Replicate once and exit (as opposed to the default behavior of daemon). _(CLI-only)_
| `version`         | Output version information and quit. _(CLI-only)_

\* = Required parameter

Additionally, the following options are available for advanced users. It is not recommended you change these values unless you have a specific use case.

|       Option      | Description |
| ----------------- |------------ |
| `status-dir`      | The path in the KV store that is used to store the replication statuses. The default value is "service/consul-replicate/statuses".

### Command Line
The CLI interface supports all of the options detailed above.

Replicate all keys under "global" from the nyc1 data center:

```shell
$ consul-replicate \
  -prefix "global@nyc1"
```

Replicate all keys under "global" from the nyc1 data center, renaming the key to "default" in the replicated stores:

```shell
$ consul-replicate \
  -prefix "global@nyc1:default"
```

Replicate all keys under "global" from the nyc1 data center, but do not poll or watch for changes (just do it one time):

```shell
$ consul-replicate \
  -prefix "global@nyc1" \
  -once
```

Replicate all keys under "global" from the nyc1 data center, but exclude the global/private prefix:

```shell
$ consul-replicate \
  -prefix "global@nyc1" \
  -exclude "global/private" \
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
log_level = "debug"

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

exclude {
  source = "vault/core/lock"
}
```

If a directory is given instead of a file, all files in the directory (recursively) will be merged in [lexical order](http://golang.org/pkg/path/filepath/#Walk). So if multiple files declare a "consul" key for instance, the last one will be used.

**Commands specified on the command line take precedence over those defined in a config file!**


Leader Election
---------------
Early versions of [Consul Replicate][] allowed multiple instances to run per data center for redundancy and high-availability. They used Consul's [leader election][] to elect a single node to perform the replication and gracefully fail over. As of Consul Replicate v0.2.0, Consul Replicate does not select a leader for you. To select a leader and lock, run the command with `consul lock` (requires Consul 0.5+):

```shell
consul lock locks/replicate consul-replicate -prefix ...
```


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
<timestamp> [INFO] (runner) creating new runner (once: false)
<timestamp> [INFO] (runner) creating consul/api client
<timestamp> [DEBUG] (runner) setting address to 127.0.0.1:8500
<timestamp> [DEBUG] (runner) setting basic auth
<timestamp> [INFO] (runner) creating Watcher
<timestamp> [INFO] (runner) starting
<timestamp> [INFO] (watcher) adding "storeKeyPrefix(global@dc1)"
<timestamp> [DEBUG] (watcher) "storeKeyPrefix(global@dc1)" starting
<timestamp> [DEBUG] (view) "storeKeyPrefix(global@dc1)" starting fetch
<timestamp> [DEBUG] ("storeKeyPrefix(global@dc1)") querying Consul with ...
<timestamp> [DEBUG] ("storeKeyPrefix(global@dc1)") Consul returned 5 key pairs
<timestamp> [INFO] (view) "storeKeyPrefix(global@dc1)" received data from consul
<timestamp> [INFO] (runner) quiescence timers starting
<timestamp> [DEBUG] (view) "storeKeyPrefix(global@dc1)" starting fetch
<timestamp> [DEBUG] ("storeKeyPrefix(global@dc1)") querying Consul with ...
<timestamp> [DEBUG] (runner) updated key "backup/five"
<timestamp> [DEBUG] (runner) updated key "backup/four"
<timestamp> [DEBUG] (runner) updated key "backup/one"
<timestamp> [DEBUG] (runner) updated key "backup/three"
<timestamp> [DEBUG] (runner) updated key "backup/two"
<timestamp> [INFO] (runner) replicated 5 updates, 0 deletes
# ...
```


FAQ
---
**Q: Can I use this for master-master replication?**<br>
A: No, a proper leader will never be elected.


Contributing
------------
To hack on Consul Replicate, you will need a modern [Go][] environment of version 1.7 or higher. To compile the `consul-replicate` binary and run the test suite, simply execute:

```shell
$ make
```

This will compile the `consul-replicate` binary into `bin/consul-replicate` and run the test suite.

If you just want to run the tests:

```shell
$ make test
```

Or to run a specific test in the suite:

```shell
go test ./... -run SomeTestFunction_name
```

Submit Pull Requests and Issues to the [Consul Replicate project on GitHub][Consul Replicate].


[Consul]: https://www.consul.io/ "Service discovery and configuration made easy"
[leader election]: https://www.consul.io/docs/guides/leader-election.html "Consul Leader election"
[Releases]: https://github.com/hashicorp/consul-replicate/releases "Consul Replicate releases page"
[HCL]: https://github.com/hashicorp/hcl "HashiCorp Configuration Language (HCL)"
[Go]: https://golang.org "Go the language"
[Consul ACLs]: https://www.consul.io/docs/internals/acl.html "Consul ACLs"
[Consul Replicate]: https://github.com/hashicorp/consul-replicate "Consul Replicate on GitHub"
