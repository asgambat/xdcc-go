package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"xdcc-go/internal/cli"
	"xdcc-go/internal/downloader"
	"xdcc-go/internal/entities"
	"xdcc-go/internal/search"
)

func main() {
	var (
		engineName       string
		server           string
		out              string
		throttle         string
		connectTimeout   int
		stallTimeout     int
		fallbackChannel  string
		waitTime         int
		username         string
		channelJoinDelay int
		verbosity        int
		quietLevel       int
		extFilter        string
		botFilter        string
		dnsServer        string
		compact          bool
	)

	cmd := &cobra.Command{
		Use:   "xdcc-browse <search_term>",
		Short: "Search for XDCC packs and download interactively",
		Long: `xdcc-browse searches for XDCC packs, optionally filters the results,
displays a numbered list, and then downloads the selected pack(s).

Filters (applied before the selection menu):
  --ext   keep only files with the given extension(s)  (e.g. --ext=mkv,avi)
  --bot   keep only packs from bots whose name contains the given substring

Selection syntax (after the list is shown):
  3        single pack
  1-5      range (packs 1 through 5)
  1+5      count (5 consecutive packs starting from 1, i.e. 1-5)
  1,3,7    comma-separated list
  all      download everything in the list

Verbosity levels:
  (default)  show connection and download progress
  -v         also show bot notices, channel joins, WHOIS results
  -vv        full debug (DNS, DCC internals, all IRC events)
  -q         hide connection info; show only errors, bot notices and progress
  -qq        suppress all output

If -q and -v are used together, -q takes precedence and -v is ignored.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			term := args[0]

			engine := search.EngineByName(engineName, false)
			if engine == nil {
				return fmt.Errorf("unknown search engine %q. Available: %s",
					engineName, strings.Join(search.AvailableEngines(), ", "))
			}

			results, err := engine.Search(term)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			// Filter by extension if requested
			if extFilter != "" {
				results = filterByExtension(results, extFilter)
			}

			// Filter by bot name if requested
			if botFilter != "" {
				results = filterByBot(results, botFilter)
			}

			// Compact results if requested
			if compact {
				before := len(results)
				results = entities.CompactPacks(results)
				if len(results) < before {
					fmt.Fprintf(os.Stderr, "Compact: %d results reduced to %d\n", before, len(results))
				}
			}

			if len(results) == 0 {
				fmt.Println("No results found.")
				return nil
			}

			// Display results
			fmt.Printf("\nFound %d result(s):\n\n", len(results))
			for i, pack := range results {
				fmt.Printf("  [%3d] %s [%s] bot: %s\n", i+1,
					pack.Filename,
					entities.HumanReadableBytes(pack.Size),
					pack.Bot)
			}

			// Interactive selection
			selected, err := selectPacks(results)
			if err != nil {
				return err
			}
			if len(selected) == 0 {
				fmt.Println("No packs selected.")
				return nil
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			entities.PreparePacks(selected, out)

			// Explicit --server overrides the bot-prefix → server auto-detection
			// (TLT→williamgattone, WeC→explosionirc) applied by PreparePacks.
			// Without this flag, the correct server is chosen automatically.
			if server != "" {
				srv := entities.ParseIrcServer(server)
				for _, p := range selected {
					p.Server = srv
				}
			}

			throttleBytes, err := entities.ParseThrottle(throttle)
			if err != nil {
				return fmt.Errorf("invalid throttle value %q: %w", throttle, err)
			}

			downloader.DownloadPacks(ctx, selected, downloader.Options{
				ConnectTimeout:   connectTimeout,
				StallTimeout:     stallTimeout,
				FallbackChannel:  fallbackChannel,
				ThrottleBytes:    throttleBytes,
				WaitTime:         waitTime,
				Username:         username,
				ChannelJoinDelay: channelJoinDelay,
				Verbosity:        cli.VerbosityLevel(verbosity, quietLevel),
				DNSServer:        dnsServer,
			})
			return nil
		},
	}

	cmd.Flags().StringVarP(&engineName, "search-engine", "e", "xdcc-eu",
		"Search engine to use: nibl, xdcc-eu, subsplease")
	cmd.Flags().StringVarP(&server, "server", "s", "",
		"Override IRC server for all selected packs (host or host:port). Default: use server from search result")
	cmd.Flags().StringVarP(&out, "out", "o", "",
		"Output directory or file path (defaults to current directory with pack filename)")
	cmd.Flags().StringVarP(&throttle, "throttle", "t", "-1",
		"Download speed limit in bytes/s (e.g. 512K, 2M, 1G). -1 = unlimited")
	cmd.Flags().IntVarP(&connectTimeout, "connect-timeout", "C", 120,
		"Seconds to wait for the bot to initiate the DCC transfer")
	cmd.Flags().IntVarP(&stallTimeout, "stall-timeout", "S", 60,
		"Seconds of no transfer progress before aborting. 0 = disabled")
	cmd.Flags().StringVarP(&fallbackChannel, "fallback-channel", "f", "",
		"IRC channel to join if WHOIS returns no channels for the bot")
	cmd.Flags().IntVarP(&waitTime, "wait-time", "w", 0,
		"Extra seconds to wait before sending the XDCC request")
	cmd.Flags().StringVarP(&username, "username", "u", "",
		"IRC nickname to use (a random suffix is always appended; default: random)")
	cmd.Flags().IntVarP(&channelJoinDelay, "channel-join-delay", "D", -1,
		"Seconds to wait after connecting before sending WHOIS (-1 = random 5-10s)")
	cmd.Flags().CountVarP(&verbosity, "verbose", "v", "Increase verbosity: -v shows bot notices, -vv shows full debug info")
	cmd.Flags().CountVarP(&quietLevel, "quiet", "q", "Reduce output: -q hides connection info (keeps errors/notices/progress), -qq suppresses all output")
	cmd.Flags().StringVarP(&extFilter, "ext", "x", "",
		"Filter results by file extension(s), comma-separated (e.g. mkv,avi,mp4)")
	cmd.Flags().StringVarP(&botFilter, "bot", "b", "",
		"Filter results by bot name substring, case-insensitive (e.g. WOND)")
	cmd.Flags().StringVarP(&dnsServer, "dns-server", "d", "",
		"Fallback DNS resolver used when system DNS is blocked (host:port, default: 8.8.8.8:53)")
	cmd.Flags().BoolVarP(&compact, "compact", "c", false,
		"Remove duplicate results with same filename, size and bot family")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// filterByBot returns only packs whose bot name contains the given substring (case-insensitive).
func filterByBot(packs []*entities.XDCCPack, substr string) []*entities.XDCCPack {
	sub := strings.ToLower(substr)
	var out []*entities.XDCCPack
	for _, p := range packs {
		if strings.Contains(strings.ToLower(p.Bot), sub) {
			out = append(out, p)
		}
	}
	return out
}
// extList is a comma-separated string like "mkv,avi,mp4".
func filterByExtension(packs []*entities.XDCCPack, extList string) []*entities.XDCCPack {
	exts := make(map[string]bool)
	for _, e := range strings.Split(extList, ",") {
		e = strings.TrimSpace(strings.ToLower(e))
		if e != "" {
			if !strings.HasPrefix(e, ".") {
				e = "." + e
			}
			exts[e] = true
		}
	}
	var out []*entities.XDCCPack
	for _, p := range packs {
		ext := strings.ToLower(filepath.Ext(p.Filename))
		if exts[ext] {
			out = append(out, p)
		}
	}
	return out
}

// verbosityLevel moved to internal/cli package.

// selectPacks prompts the user to select one or more packs from the results list.
// Accepts: single number (3), range (1-5), comma list (1,3,5), or "all".
// On invalid input, prints an error and re-prompts until valid input is given.
func selectPacks(results []*entities.XDCCPack) ([]*entities.XDCCPack, error) {
	reader := bufio.NewReader(os.Stdin)
	prompt := fmt.Sprintf("\nEnter selection (number, range 1-%d, count 1+5, list 1,3,5, or 'all'): ", len(results))

	for {
		fmt.Print(prompt)
		input, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		input = strings.TrimSpace(input)

		if strings.ToLower(input) == "all" {
			return results, nil
		}

		selected, parseErr := parseSelection(input, results)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", parseErr)
			continue
		}
		return selected, nil
	}
}

// parseSelection parses a selection string (e.g. "1", "1-3", "1,3,5") and
// returns the corresponding packs. Returns an error if the input is invalid.
func parseSelection(input string, results []*entities.XDCCPack) ([]*entities.XDCCPack, error) {
	var selected []*entities.XDCCPack
	seen := make(map[int]bool)

	addIdx := func(i int) error {
		if i < 1 || i > len(results) {
			return fmt.Errorf("index %d out of range (1-%d)", i, len(results))
		}
		if !seen[i] {
			seen[i] = true
			selected = append(selected, results[i-1])
		}
		return nil
	}

	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "+") {
			bounds := strings.SplitN(part, "+", 2)
			start, e1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			count, e2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if e1 != nil || e2 != nil || count < 1 {
				return nil, fmt.Errorf("invalid selection: %q", part)
			}
			for i := start; i < start+count; i++ {
				if err := addIdx(i); err != nil {
					return nil, err
				}
			}
		} else if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, e1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			end, e2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if e1 != nil || e2 != nil {
				return nil, fmt.Errorf("invalid selection: %q", part)
			}
			if start > end {
				return nil, fmt.Errorf("invalid range: start %d > end %d", start, end)
			}
			for i := start; i <= end; i++ {
				if err := addIdx(i); err != nil {
					return nil, err
				}
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid selection: %q", part)
			}
			if err := addIdx(n); err != nil {
				return nil, err
			}
		}
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no valid packs in selection")
	}

	return selected, nil
}
