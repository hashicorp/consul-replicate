package main // import "github.com/hashicorp/consul-replicate"

import (
	"os"
)

// Name is the exported name of this application.
const Name = "consul-replicate"

// Version is the current version of this application.
const Version = "0.2.0"

func main() {
	cli := NewCLI(os.Stdout, os.Stderr)
	os.Exit(cli.Run(os.Args))
}
