package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"xdcc-go/internal/entities"
	"xdcc-go/internal/search"
)

func main() {
	var (
		engineName string
		verbosity  int
	)

	cmd := &cobra.Command{
		Use:   "xdcc-search <search_term> [engine]",
		Short: "Search for XDCC packs and print download commands",
		Long: `xdcc-search queries an XDCC search engine and prints one result per line
with the corresponding xdcc-dl command ready to copy-paste.

The engine argument is optional; default is xdcc-eu.
Available engines: ` + strings.Join(search.AvailableEngines(), ", ") + `

Output format per result:
  <filename> [<size>] (xdcc-dl "<message>" [--server <host>])

Verbosity levels:
  (default)  results only
  -v         also show search engine debug info (e.g. HTTP requests)`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			term := args[0]
			if len(args) == 2 {
				engineName = args[1]
			}

			engine := search.EngineByName(engineName, verbosity >= 1)
			if engine == nil {
				return fmt.Errorf("unknown search engine %q. Available: %s",
					engineName, strings.Join(search.AvailableEngines(), ", "))
			}

			results, err := engine.Search(term)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			if len(results) == 0 {
				fmt.Fprintln(os.Stderr, "No results found.")
				return nil
			}

			for _, pack := range results {
				msg := pack.GetRequestMessage(true)
				line := fmt.Sprintf("%s [%s] (xdcc-dl \"%s\")",
					pack.Filename,
					entities.HumanReadableBytes(pack.Size),
					msg)
				if pack.Server.Address != "irc.rizon.net" {
					line = fmt.Sprintf("%s [%s] (xdcc-dl \"%s\" --server %s)",
						pack.Filename,
						entities.HumanReadableBytes(pack.Size),
						msg,
						pack.Server.Address)
				}
				fmt.Println(line)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&engineName, "search-engine", "e", "xdcc-eu",
		"Search engine to use: nibl, xdcc-eu, ixirc, subsplease. Can also be passed as second positional argument")
	cmd.Flags().CountVarP(&verbosity, "verbose", "v", "Increase verbosity: -v shows search engine debug info")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
