// The aoci-code process entry
// Index entry: main.go[Cmd.Root.9.T]
//
// Responsibility: only calls cli.Execute(), with zero business logic.
// Iron rule: no log anywhere in the repository may touch stdout; under the mcp subcommand stdout is the JSON-RPC protocol stream,
// and any leftover println or log poisons the protocol stream and the MCP client disconnects silently.
// The Go standard library log writes to stderr by default; setting it here explicitly once makes that default a convention that does not rely on chance.
package main

import (
	"log"
	"os"

	"github.com/aoci-spec/aoci-code/internal/cli"
)

func main() {
	// Explicitly fix the log output to stderr (see the iron rule above)
	log.SetOutput(os.Stderr)

	// Every command registration, argument parse, and exit code mapping happens inside internal/cli
	cli.Execute()
}
