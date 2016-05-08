Consul Replicate Changelog
==========================

# v0.2.0.dev (Unreleased)

IMPROVEMENTS:

  * Vendor dependencies to allow easier building from source
  * Fix race conditions [GH-39]

BUG FIXES:

  * Trim leading slashes on prefixes
  * Fix config merge ordering [GH-23]
  * Fix a number of config-related issues [GH-43]
  * Fix issues with keys vs. folder syncing [GH-25]

# v0.2.0 (February 26, 2015)

BREAKING CHANGES:

  * `-status` is no longer used and is not replaced - `-status-dir` is the
    closest replacement
  * Remove support for leader election - use `consul lock` instead (requires
    Consul 0.5+)

DEPRECATIONS:

  * `-src` is now part of the `-prefix` key
  * `-dst` is now part of the `-prefix` key
  * `-lock` is not used - run with `consul lock` instead
  * `-service` is not used - run with `consul lock` instead
  *  `-addr` is deprecated - use `-consul` instead

FEATURES:

  * Add support for specifying multiple prefixes via the `-prefix` option - the
    new `-prefix` option can be used to specify the source prefix and datacenter
    and optional destination prefix
  * Add support for using an HCL configuration file - this is especially helpful
    if you need to replicate multiple prefixes or have custom options that are
    cumbersome to specify via the CLI
  * Add support for specifying basic authentication
  * Add support for specifying the maximum staleness of a query
  * Add support for SSL
  * Add support for logging to syslog
  * Add support for specifying quiescence timers
  * Add support for specifying a retry interval
  * Add support for multiple log levels
  * Add support for running once and quitting
  * Use Consul Template's watching library for performance and durability

IMPROVEMENTS:

  * Improve test coverage, complete with integration tests

BUG FIXES:

  * Update README with examples and more documentation
  * Gracefully shut down when interrupted (prevents partial key replication)


# v0.1.0 (June 19, 2014)

  * Initial release
