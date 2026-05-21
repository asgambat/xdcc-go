// Package irc implements the XDCC IRC client using the girc library.
// A single Client can download multiple packs sequentially on the same IRC
// connection, rejoining channels only when needed.
package irc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lrstanley/girc"
	"xdcc-go/internal/entities"
)

// ---------------------------------------------------------------------------
// Client struct
// ---------------------------------------------------------------------------

// Client manages the download of one or more XDCC packs on a single IRC
// connection. Packs on the same server are downloaded without disconnecting;
// channels already joined are not rejoined.
type Client struct {
	ctx       context.Context
	packs     []*entities.XDCCPack
	opts      DownloadOptions
	verbosity int // 0=normal, 1=verbose, 2=debug, -1=quiet

	// IRC connection (reset on reconnect)
	irc            *girc.Client
	ircErrCh       chan error     // receives error from irc.Connect() goroutine
	connectedCh    chan struct{}  // closed on CONNECTED event
	joinedChannels map[string]bool // channels joined in this connection (cleared on reconnect)
	connectTime    time.Time

	// When true, the client uses an existing connection managed externally
	// (e.g. by ircmanager). connect() is skipped; the caller must call
	// SetExistingClient() to provide the girc.Client before DownloadAll().
	usingExistingConn bool

	// Current pack index (set before each pack download)
	packIdxVal atomic.Int32

	// Per-pack state (reset via resetForPack between packs)
	mu                 sync.Mutex
	peerAddr           string   // stored on DCC SEND, reused on DCC ACCEPT
	dccConn            net.Conn
	dccFile            *os.File
	progress           int64
	filesize           int64
	dccTimestamp       time.Time
	downloading        bool
	downloadError      error
	lastBotNotice      string
	downStartTime      time.Time

	ackQueue        chan []byte
	downloadDone    chan struct{} // closed when pack finishes (success or error)
	downloadStarted chan struct{} // closed when DCC TCP connection is established
	closeOnce       sync.Once
	startOnce       sync.Once

	// WHOIS flow control (per-pack, reset in resetForPack)
	messageSent        atomic.Bool
	whoisFoundChannels atomic.Bool // WHOIS found at least one channel
	needsJoin          atomic.Bool // we sent a JOIN and must wait for confirmation

	// stall detection: unix nanoseconds of last received byte
	lastActivity atomic.Int64

	logger Logger
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewClient creates a new XDCC Client that will download all packs in order.
// packs must all belong to the same IRC server.
// verbosity: -1=quiet, 0=normal, 1=verbose (-v), 2=debug (-vv).
func NewClient(ctx context.Context, packs []*entities.XDCCPack, opts DownloadOptions, verbosity int) *Client {
	if opts.ChannelJoinDelay < 0 {
		opts.ChannelJoinDelay = randN(6) + 5
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = 120
	}
	if opts.StallTimeout < 0 {
		opts.StallTimeout = 0
	}
	if opts.DNSServer == "" {
		opts.DNSServer = "8.8.8.8:53"
	}
	logger := opts.Logger
	if logger == nil {
		logger = defaultLogger()
	}
	return &Client{
		ctx:       ctx,
		packs:     packs,
		opts:      opts,
		verbosity: verbosity,
		logger:    logger,
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// SetExistingClient configures the client to use an already-established IRC
// connection instead of creating its own. The caller is responsible for
// managing the connection lifecycle (e.g. the ircmanager keeps the connection
// alive after the download completes).
//
// Must be called before DownloadAll().
func (c *Client) SetExistingClient(irc *girc.Client) {
	c.usingExistingConn = true
	c.irc = irc
	c.connectedCh = make(chan struct{})
	c.joinedChannels = make(map[string]bool)
	c.ircErrCh = make(chan error, 1)
	c.registerHandlers()
	close(c.connectedCh) // Already connected — signal immediately
}

// DownloadAll downloads all packs sequentially, reusing the IRC connection
// for packs on the same server. Returns one PackResult per pack.
func (c *Client) DownloadAll() []PackResult {
	c.logf("=== Starting XDCC download session ===")
	c.logf("Server: %s:%d", c.packs[0].Server.Address, c.packs[0].Server.Port)
	c.logf("Total packs to download: %d", len(c.packs))
	
	results := make([]PackResult, len(c.packs))

	if !c.usingExistingConn {
		if err := c.connect(); err != nil {
			c.logf("ERROR: Failed to connect to IRC server: %v", err)
			for i := range results {
				results[i].Error = err
			}
			return results
		}
	} else {
		c.logf("Using existing persistent IRC connection")
	}

	if !c.usingExistingConn {
		defer func() {
			c.logf("=== Closing IRC connection ===")
			c.irc.Close()
			<-c.ircErrCh
		}()
	}

	closeConn := func() {
		if !c.usingExistingConn {
			c.irc.Close()
			select {
			case <-c.ircErrCh:
			case <-time.After(5 * time.Second):
			}
		}
	}

	for i := range c.packs {
		select {
		case <-c.ctx.Done():
			for j := i; j < len(results); j++ {
				results[j].Error = ErrCancelled
			}
			closeConn()
			return results
		default:
		}
		if i > 0 {
			c.debugf("Waiting 3s before next pack")
			select {
			case <-c.ctx.Done():
				for j := i; j < len(results); j++ {
					results[j].Error = ErrCancelled
				}
				closeConn()
				return results
			case <-time.After(3 * time.Second):
			}
		}
		results[i] = c.downloadPackAtIndex(i, 0)
		// Fatal errors: propagate to all remaining packs
		if results[i].Error != nil {
			if errors.Is(results[i].Error, ErrServerUnreachable) ||
				errors.Is(results[i].Error, ErrUnrecoverable) ||
				errors.Is(results[i].Error, ErrCancelled) {
				for j := i + 1; j < len(results); j++ {
					results[j].Error = results[i].Error
				}
				break
			}
		}
	}

	if !c.usingExistingConn {
		c.irc.Close()
		// Drain ircErrCh so the goroutine can exit
		select {
		case <-c.ircErrCh:
		case <-time.After(5 * time.Second):
		}
	}
	return results
}

// LastBotNotice returns the last NOTICE received from the bot for the
// current pack. Safe to call after DownloadAll returns.
func (c *Client) LastBotNotice() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastBotNotice
}

// ---------------------------------------------------------------------------
// Connection management
// ---------------------------------------------------------------------------

func (c *Client) connect() error {
	// When using an existing connection managed externally, skip connection entirely.
	if c.usingExistingConn {
		return nil
	}

	server := c.packs[0].Server

	// Resolve the hostname to all valid IPs so we can try each one in order.
	resolvedIPs, err := c.resolveAllHosts(server.Address)
	if err != nil {
		return err
	}

	nick := c.opts.Username
	if nick == "" {
		nick = randomUsername()
	} else {
		nick = nick + randomSuffix(3)
	}

	var lastErr error
	for i, ip := range resolvedIPs {
		if len(resolvedIPs) > 1 {
			c.infof("Connecting to %s:%d as '%s' (IP %d/%d: %s)",
				server.Address, server.Port, nick, i+1, len(resolvedIPs), ip)
		} else {
			c.infof("Connecting to %s:%d as '%s'", server.Address, server.Port, nick)
		}

		c.connectedCh = make(chan struct{})
		c.joinedChannels = make(map[string]bool)
		c.ircErrCh = make(chan error, 1)

		c.irc = girc.New(girc.Config{
			Server:      ip, // use resolved IP to avoid repeating a blocked DNS lookup
			Port:        server.Port,
			Nick:        nick,
			User:        nick,
			Name:        nick,
			PingDelay:   30 * time.Second,
			PingTimeout: 60 * time.Second,
		})
		c.registerHandlers()
		go func() { c.ircErrCh <- c.irc.Connect() }()

		timeout := time.Duration(c.opts.ConnectTimeout+30) * time.Second
		select {
		case <-c.connectedCh:
			return nil
		case connErr := <-c.ircErrCh:
			if connErr != nil {
				if isConnectError(connErr) {
					lastErr = connErr
					if i < len(resolvedIPs)-1 {
						c.noticef("IP %s failed (%v), trying next IP...", ip, connErr)
						continue
					}
					return fmt.Errorf("%w: all %d IPs for %s failed (last: %v)",
						ErrServerUnreachable, len(resolvedIPs), server.Address, lastErr)
				}
				return connErr
			}
			return fmt.Errorf("IRC connection closed before CONNECTED event")
		case <-c.ctx.Done():
			c.irc.Close()
			return ErrCancelled
		case <-time.After(timeout):
			c.irc.Close()
			lastErr = fmt.Errorf("connection to %s timed out", ip)
			if i < len(resolvedIPs)-1 {
				c.noticef("IP %s timed out, trying next IP...", ip)
				continue
			}
			return fmt.Errorf("%w: all %d IPs for %s timed out",
				ErrServerUnreachable, len(resolvedIPs), server.Address)
		}
	}

	// Should not be reached, but handle defensively.
	return fmt.Errorf("%w: %v", ErrServerUnreachable, lastErr)
}

func (c *Client) reconnect() error {
	// When using an existing connection, the persistent connection handles
	// reconnection itself. Just return the error to let the queue manager
	// handle retry logic at a higher level.
	if c.usingExistingConn {
		return fmt.Errorf("cannot reconnect on persistent connection")
	}

	c.infof("Reconnecting to IRC...")
	c.irc.Close()
	// Drain ircErrCh (may have been consumed already; best-effort)
	select {
	case <-c.ircErrCh:
	case <-time.After(3 * time.Second):
	}
	return c.connect()
}

// ---------------------------------------------------------------------------
// Per-pack download
// ---------------------------------------------------------------------------

func (c *Client) currentPack() *entities.XDCCPack {
	return c.packs[c.packIdxVal.Load()]
}

func (c *Client) resetForPack() {
	c.mu.Lock()
	c.peerAddr = ""
	if c.dccConn != nil {
		c.dccConn.Close()
		c.dccConn = nil
	}
	if c.dccFile != nil {
		c.dccFile.Close()
		c.dccFile = nil
	}
	c.progress = 0
	c.filesize = 0
	c.downloading = false
	c.downloadError = nil
	c.lastBotNotice = ""
	c.downStartTime = time.Time{}
	c.mu.Unlock()

	c.messageSent.Store(false)
	c.whoisFoundChannels.Store(false)
	c.needsJoin.Store(false)
	c.lastActivity.Store(0)
	c.downloadDone = make(chan struct{})
	c.downloadStarted = make(chan struct{})
	c.ackQueue = make(chan []byte, 256)
	c.closeOnce = sync.Once{}
	c.startOnce = sync.Once{}
}

func (c *Client) downloadPackAtIndex(idx int, retryCount int) PackResult {
	if retryCount > 3 {
		return PackResult{Error: fmt.Errorf("giving up on pack %d after 3 retries",
			c.packs[idx].PackNumber)}
	}

	c.packIdxVal.Store(int32(idx))
	c.resetForPack()
	pack := c.currentPack()

	c.logf("--- Starting pack download: %s (pack #%d) from bot %s ---", pack.Filename, pack.PackNumber, pack.Bot)

	// Channel-join delay only on first connection (not between packs)
	if idx == 0 {
		c.logf("Waiting %ds before WHOIS (channel join delay)", c.opts.ChannelJoinDelay)
		select {
		case <-c.ctx.Done():
			return PackResult{Error: ErrCancelled}
		case <-time.After(time.Duration(c.opts.ChannelJoinDelay) * time.Second):
		}
	}

	c.logf("→ Sending WHOIS query for bot: %s", pack.Bot)
	c.irc.Cmd.Whois(pack.Bot)

	err := c.waitForCurrentPack()
	if err == nil {
		return PackResult{FilePath: pack.GetFilepath()}
	}

	switch {
	case errors.Is(err, ErrPackAlreadyReq):
		fmt.Println("Pack already requested. Waiting 60 seconds before retrying...")
		select {
		case <-c.ctx.Done():
			return PackResult{Error: ErrCancelled}
		case <-time.After(60 * time.Second):
		}
		return c.downloadPackAtIndex(idx, retryCount+1)

	case errors.Is(err, ErrTimeout), errors.Is(err, ErrDownloadFailed):
		fmt.Printf("Retrying pack #%d (attempt %d/3)...\n", pack.PackNumber, retryCount+1)
		if err2 := c.reconnect(); err2 != nil {
			return PackResult{Error: err2}
		}
		return c.downloadPackAtIndex(idx, retryCount+1)
	}

	c.mu.Lock()
	notice := c.lastBotNotice
	c.mu.Unlock()
	return PackResult{Error: err, LastBotNotice: notice}
}

func (c *Client) waitForCurrentPack() error {
	// Phase 1: wait for DCC transfer to start.
	// Covers: WHOIS response + channel join + bot response + WaitTime.
	connectTimeout := time.Duration(c.opts.ConnectTimeout+c.opts.WaitTime+30) * time.Second
	c.debugf("Waiting up to %s for bot to initiate DCC transfer", connectTimeout)

	select {
	case <-c.downloadStarted:
		c.debugf("Transfer started, switching to stall detection")
	case <-c.downloadDone:
		return c.downloadError
	case <-c.ctx.Done():
		c.finishWithError(ErrCancelled)
		return ErrCancelled
	case err := <-c.ircErrCh:
		// IRC connection died before transfer started; treat as timeout so
		// downloadPackAtIndex will reconnect and retry.
		if err != nil && c.downloadError == nil {
			if isConnectError(err) {
				return fmt.Errorf("%w: %v", ErrServerUnreachable, err)
			}
			return ErrTimeout
		}
		return c.downloadError
	case <-time.After(connectTimeout):
		c.finishWithError(ErrTimeout)
		return ErrTimeout
	}

	// Phase 2: DCC transfer is a direct TCP connection — it can survive
	// IRC disconnect. Just wait for completion.
	if c.opts.StallTimeout > 0 {
		go c.stallWatcher()
	}
	select {
	case <-c.downloadDone:
	case <-c.ctx.Done():
		c.mu.Lock()
		if c.dccConn != nil {
			c.dccConn.Close()
		}
		c.mu.Unlock()
		c.finishWithError(ErrCancelled)
	}
	return c.downloadError
}

// ---------------------------------------------------------------------------
// Finish helpers
// ---------------------------------------------------------------------------

// finishSuccess records a successful download. Does NOT close the IRC
// connection so subsequent packs can reuse it.
func (c *Client) finishSuccess() {
	elapsed := time.Since(c.downStartTime)
	speedStr := formatSpeed(float64(c.filesize) / elapsed.Seconds())
	fmt.Printf("\nFile %s downloaded successfully in %s at %s\n",
		c.currentPack().Filename,
		formatDuration(elapsed),
		speedStr)
	c.closeOnce.Do(func() {
		if c.downloadDone != nil {
			close(c.downloadDone)
		}
	})
}

// finishWithNotice stores a bot notice and then calls finishWithError.
func (c *Client) finishWithNotice(err error, notice string) {
	c.mu.Lock()
	c.lastBotNotice = notice
	c.mu.Unlock()
	c.finishWithError(err)
}

// finishWithError records a download error. Does NOT close the IRC
// connection so the session can retry or continue with the next pack.
// The first error wins: subsequent calls are ignored (sync.Once guards the channel close).
func (c *Client) finishWithError(err error) {
	c.mu.Lock()
	if c.downloadError == nil {
		c.downloadError = err
	}
	c.mu.Unlock()
	c.closeOnce.Do(func() {
		if c.downloadDone != nil {
			close(c.downloadDone)
		}
	})
}

// ---------------------------------------------------------------------------
// Logging
// ---------------------------------------------------------------------------

// infof prints at verbosity >= 0 (default and above). Use for connection status
// and download progress that is suppressed only in quiet mode (-q / -qq).
func (c *Client) infof(format string, args ...interface{}) {
	if c.verbosity >= 0 {
		c.logger.Printf(format, args...)
	}
}

// noticef prints at verbosity >= -1 (quiet and above).
// Use for errors, bot messages, and status that matter even in quiet mode.
func (c *Client) noticef(format string, args ...interface{}) {
	if c.verbosity >= -1 {
		c.logger.Printf(format, args...)
	}
}

// logf prints at verbosity >= 1 (-v). Use for channel joins, WHOIS results,
// DCC negotiation messages.
func (c *Client) logf(format string, args ...interface{}) {
	if c.verbosity >= 1 {
		c.logger.Printf(format, args...)
	}
}

// debugf prints at verbosity >= 2 (-vv). Use for low-level details: DNS,
// DCC internals, raw IRC event flow.
func (c *Client) debugf(format string, args ...interface{}) {
	if c.verbosity >= 2 {
		c.logger.Printf(format, args...)
	}
}
