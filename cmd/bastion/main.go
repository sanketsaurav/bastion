// Command bastion is a local, config-driven CLI for operating a personal
// development box in the cloud. See SPEC.md.
package main

import (
	"os"

	"github.com/sanketsaurav/bastion/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
