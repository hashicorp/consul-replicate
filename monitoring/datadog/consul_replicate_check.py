#!/usr/bin/env python
import json
import urllib2
import subprocess
from checks import AgentCheck


class ConsulReplicateCheck(AgentCheck):
    metrics_prefix = 'consul-replicate.'

    def check(self, instance):
        my_dc = self.read_my_dc()
        tags = ["dc:" + my_dc]

        error = self.replicate_running(tags)
        try:
            master_dc = self.read_kv(instance['master_dc_path'])
        except urllib2.HTTPError as e:
            print("Failed to read the value for replication master DC at path %s: %s"
                  % (instance['master_dc_path'], e))
            return False

        self.gauge(self.metrics_prefix + "in_master_dc",
                   1 if master_dc == my_dc else 0, tags)

        if master_dc == my_dc:
            # No point checking paths
            return error

        for path in instance['check_paths']:
            error = error and self.check_path(path, master_dc, tags)
        return error

    def read_my_dc(self):
        agent_self = urllib2.urlopen("http://127.0.0.1:8500/v1/agent/self")
        j = json.load(agent_self)
        agent_self.close()
        return j['Config']['Datacenter']

    def replicate_running(self, tags):
        retcode = subprocess.call(
            ["/bin/ps", "-C", "consul-replicate", "--no-headers"])
        if retcode != 0 and retcode != 1:
            return False
        self.gauge(self.metrics_prefix + "running", 1 - retcode, tags)
        return True

    def check_path(self, path, master_dc, tags):
        tags_with_prefix = list(tags)
        tags_with_prefix.append("prefix:" + path.split('/')[0])
        readable = 1

        try:
            equal = self.read_kv(path) == self.read_kv(path, master_dc)
            self.gauge(self.metrics_prefix + "value_mismatch",
                       0 if equal else 1, tags_with_prefix)
        except urllib2.HTTPError as e:
            print("Failed to check values at path %s: %s" % (path, e))
            readable = 0

        self.gauge(self.metrics_prefix + "value_readable",
                   readable, tags_with_prefix)

        return True

    def read_kv(self, path, dc=None):
        url = "http://127.0.0.1:8500/v1/kv/" + path + "?raw"
        if dc:
            url = url + "&dc=" + dc

        kv = urllib2.urlopen(url)
        raw = kv.read()
        kv.close()
        return raw


if __name__ == '__main__':
    check, instances = ConsulReplicateCheck.from_yaml(
        '/etc/dd-agent/conf.d/consul_replicate_check.yaml')
    for instance in instances:
        print "\nRunning the check"
        check.check(instance)
        if check.has_events():
            print 'Events: %s' % (check.get_events())
        print 'Metrics: %s' % (check.get_metrics())

