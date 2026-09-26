// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package main // import "github.com/hashicorp/consul-replicate"

import "os"

func main() {
	cli := NewCLI(os.Stdout, os.Stderr)
	os.Exit(cli.Run(os.Args))
}
