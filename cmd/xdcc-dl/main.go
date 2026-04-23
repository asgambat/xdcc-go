package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"xdcc-go/internal/downloader"
	"xdcc-go/internal/entities"
)

func main() {
	var (
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
	)

	cmd := &cobra.Command{
		Use:   "xdcc-dl <message>",
		Short: "Download a file via XDCC IRC protocol",
		Long: `xdcc-dl downloads files from IRC bots using the XDCC protocol.

The message must be in the format:  /msg <bot> xdcc send #<pack>
Pack number supports ranges, steps, and comma lists:
  /msg MyBot xdcc send #5        single pack
  /msg MyBot xdcc send #1-10     packs 1 through 10
  /msg MyBot xdcc send #1-10;2   packs 1,3,5,7,9 (every 2nd)
  /msg MyBot xdcc send #1,3,7    specific packs

The IRC server is detected automatically from the bot name prefix when
possible. Use --server to override with an explicit address.

Verbosity levels:
  (default)  show connection and download progress
  -v         also show bot notices, channel joins, WHOIS results
  -vv        full debug (DNS, DCC internals, all IRC events)
  -q         hide connection info; show only errors, bot notices and progress
  -qq        suppress all output`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			message := args[0]

			packs, err := entities.ParseXDCCMessage(message, ".", server)
			if err != nil {
				return fmt.Errorf("invalid XDCC message: %w", err)
			}
			if len(packs) == 0 {
				return fmt.Errorf("no packs found in message: %s", message)
			}

			entities.PreparePacks(packs, out)

			throttleBytes, err := entities.ParseThrottle(throttle)
			if err != nil {
				return fmt.Errorf("invalid throttle value %q: %w", throttle, err)
			}

			downloader.DownloadPacks(packs, downloader.Options{
				ConnectTimeout:   connectTimeout,
				StallTimeout:     stallTimeout,
				FallbackChannel:  fallbackChannel,
				ThrottleBytes:    throttleBytes,
				WaitTime:         waitTime,
				Username:         username,
				ChannelJoinDelay: channelJoinDelay,
				Verbosity:        verbosityLevel(verbosity, quietLevel),
			})
			return nil
		},
	}

	cmd.Flags().StringVarP(&server, "server", "s", "irc.rizon.net",
		"IRC server address (host or host:port). Overrides automatic server detection from bot name")
	cmd.Flags().StringVarP(&out, "out", "o", "",
		"Output directory or file path (defaults to current directory with pack filename)")
	cmd.Flags().StringVarP(&throttle, "throttle", "t", "-1",
		"Download speed limit in bytes/s (e.g. 512K, 2M, 1G). -1 = unlimited")
	cmd.Flags().IntVarP(&connectTimeout, "connect-timeout", "c", 120,
		"Seconds to wait for the bot to initiate the DCC transfer")
	cmd.Flags().IntVarP(&stallTimeout, "stall-timeout", "S", 60,
		"Seconds of no transfer progress before aborting. 0 = disabled")
	cmd.Flags().StringVarP(&fallbackChannel, "fallback-channel", "f", "",
		"IRC channel to join if WHOIS returns no channels for the bot")
	cmd.Flags().IntVarP(&waitTime, "wait-time", "w", 0,
		"Extra seconds to wait before sending the XDCC request")
	cmd.Flags().StringVarP(&username, "username", "u", "",
		"IRC nickname to use (a random suffix is always appended; default: random)")
	cmd.Flags().IntVarP(&channelJoinDelay, "channel-join-delay", "d", -1,
		"Seconds to wait after connecting before sending WHOIS (-1 = random 5-10s)")
	cmd.Flags().CountVarP(&verbosity, "verbose", "v", "Increase verbosity: -v shows bot notices, -vv shows full debug info")
	cmd.Flags().CountVarP(&quietLevel, "quiet", "q", "Reduce output: -q hides connection info (keeps errors/notices/progress), -qq suppresses all output")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// verbosityLevel maps verbose count and quiet count to a verbosity int.
// -qq (quiet>=2) => -2 (suppress all), -q (quiet=1) => -1 (suppress info, keep errors/notices/progress)
// default => 0, -v => 1, -vv => 2
func verbosityLevel(verbose, quiet int) int {
	if quiet >= 2 {
		return -2
	}
	if quiet >= 1 {
		return -1
	}
	return verbose
}
