// bench is the Bench CLI, a thin client of the benchd HTTP API.
package main

import (
	"os"

	"github.com/d3v0psdan/bench/daemon/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
