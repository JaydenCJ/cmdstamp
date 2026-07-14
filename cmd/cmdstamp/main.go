// Command cmdstamp keeps command output in docs fresh. All behavior lives
// in internal/cli so it can be tested in-process; main only wires the real
// process streams and exit code.
package main

import (
	"os"

	"github.com/JaydenCJ/cmdstamp/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
