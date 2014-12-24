consul-replicate
===========

consul-replicate integrates with [Consul](http://www.consul.io) to perform
cross datacenter K/V replication.

This makes it possible to manage application configuration from
a central datacenter, with low-latency asyncronous replication
to other datacenters. This avoids the need for smart clients
which would need to write to all datacenters and queue writes
to handle network failures.

consul-replicate uses a highly-available, pull based architecture.
Multiple instances of consul-replicate can run per datacenter
for redundancy and high-availablilty. They use Consul's
[leader election](http://www.consul.io/docs/guides/leader-election.html)
to elect a single node to perform the replication and gracefully
failover. The active replicator watches the remote datacenter for
changes and updates the local K/V store as appropriate.

However, the daemon should not be used to attempt master-master
replication. It is not designed for this use case, and will not
ever reach a stable point. Instead, it should be used only
for master-slave replication, where a single datacenter is considered
authoritative.

## Download & Usage

Download a release from the
[releases page](#).
Run `consul-replicate` to see the usage help:

```
$ consul-replicate -h
Usage: consul-replicate [options]

  Replicates K/V data from a source datacenter to the datacenter of
  a Consul agent.

Options:

  -addr=127.0.0.1:8500  Provides the HTTP address of a Consul agent.
  -dst-prefix=global/   Provides the prefix which is the root of replicated keys
                        in the destination datacenter. Defaults to match source.
                        If multiple prefixes are provided, the number of destination
                        prefixes and source prefixes provided must match.
  -lock=path            Lock is used to provide the path in the KV store used to
                        perform leader election for the replicators. This ensures
                        a single replicator running per-DC in a high-availability
                        setup. Defaults to "service/consul-replicate/leader"
  -prefix=global/       Provides the prefix which is the root of replicated keys
                        in the source datacenter. It can also take multiple 
                        comma-separated prefixes.
  -service=name         Service sets the name of the service that is registered
                        in the catalog. Defaults to "consul-replicate"
  -src=dc               Provides the source destination to replicate from
  -status=path          Status is used to provide the path in the KV store used to
                        store our replication status. This is to checkpoint replication
                        periodically. Defaults to "service/consul-replicate/status"
  -token=""             Optional ACL token to use when reading and writing keys.
```

## Example

We run a [Consul demo cluster](http://demo.consul.io) that uses
consul-replicate to perform cross datacenter replication. Specifically,
keys in the `global/` prefix of the NYC3 datacenter are replicated to
the [SFO1](http://sfo1.demo.consul.io/ui/#/sfo1/kv/) and
[AMS2](http://ams2.demo.consul.io/ui/#/ams2/kv/) datacenters.

We can test this by doing a write to NYC3:

```
$ curl -X PUT -d test http://nyc3.demo.consul.io/v1/kv/global/foo
true
```

We should now be able to read the key from all the datacenters:

```
$ curl http://nyc3.demo.consul.io/v1/kv/global/foo
[{"CreateIndex":21123,"ModifyIndex":21123,"LockIndex":0,"Key":"global/foo","Flags":0,"Value":"dGVzdA=="}]

$ curl http://sfo1.demo.consul.io/v1/kv/global/foo
[{"CreateIndex":21123,"ModifyIndex":21123,"LockIndex":0,"Key":"global/foo","Flags":0,"Value":"dGVzdA=="}]

$ curl http://ams2.demo.consul.io/v1/kv/global/foo
[{"CreateIndex":21123,"ModifyIndex":21123,"LockIndex":0,"Key":"global/foo","Flags":0,"Value":"dGVzdA=="}]
```

### Examples with multiple prefixes

Destination prefixes will match source prefixes:

```
./consul-replicate -src=dc1 -prefix=global/,web/
```

Destination prefixes provided:

```
./consul-replicate -src=dc1 -prefix=global/,web/ -dst-prefix=global_dc2/,web_dc2/
```

| source  | destination  |
|---------|--------------|
| global/ | global_dc2/  |
| web/    | web_dc2/     |


Destination prefixes provided partially:

```
./consul-replicate -src dc1 -prefix global/,web1/,web2/ -dst-prefix=global_dc2/,,web2_dc2/
```

| source  | destination  |
|---------|--------------|
| global/ | global_dc2/  |
| web1/   | web1/        |
| web2/   | web2_dc2/    |
