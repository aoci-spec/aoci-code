// Command engine-embed shows a program embedding the AOCI-CODE kernel: it
// opens a repository through the engine package, prints what the kernel
// reads, and can serve the nine MCP tools over stdio from inside this
// process. Build it from this directory; the replace directive in go.mod
// points at the engine checkout.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/aoci-spec/aoci-code/engine"
)

func main() {
	repoPath := flag.String("repo", ".", "repository root or any directory inside it")
	serve := flag.Bool("mcp", false, "serve the nine MCP tools over stdio instead of printing a summary")
	flag.Parse()

	repo, err := engine.Open(*repoPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *serve {
		// stdout belongs to JSON-RPC from here on; keep every message on stderr.
		if err := repo.ServeStdio(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	cognition, err := repo.Cognition()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	identity, err := repo.Identity()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	summary := map[string]any{
		"engine_version": engine.Version(),
		"root":           repo.Root(),
		"layout":         cognition.Layout,
		"entries":        len(cognition.Entries),
		"identity":       identity,
	}
	if cognition.Layout == engine.LayoutVolumesV1 {
		governance, err := repo.Governance()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		summary["aligned"] = governance.Aligned
		summary["next_required_action"] = governance.NextRequiredAction
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
