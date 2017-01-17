package main // import "github.com/hashicorp/consul-replicate"

import "os"

func main() {
	cli := NewCLI(os.Stdout, os.Stderr)
	os.Exit(cli.Run(os.Args))
}
