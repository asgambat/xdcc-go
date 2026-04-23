// Package irc implements the XDCC IRC client using the girc library.
// A single Client can download multiple packs sequentially on the same IRC
// connection, rejoining channels only when needed.
package irc

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lrstanley/girc"
	"xdcc-go/internal/entities"
)

// ---------------------------------------------------------------------------
// Error types
// ---------------------------------------------------------------------------

// XDCCDownloadError represents a typed error from the XDCC download process.
type XDCCDownloadError struct {
	Kind    string
	Message string
}

func (e *XDCCDownloadError) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// Is allows errors.Is() to match by Kind, even when wrapped.
func (e *XDCCDownloadError) Is(target error) bool {
	t, ok := target.(*XDCCDownloadError)
	if !ok {
		return false
	}
	return e.Kind == t.Kind
}

var (
	ErrTimeout           = &XDCCDownloadError{Kind: "timeout", Message: "download timed out"}
	ErrBotNotFound       = &XDCCDownloadError{Kind: "bot_not_found", Message: "bot does not exist on server"}
	ErrPackAlreadyReq    = &XDCCDownloadError{Kind: "pack_already_requested", Message: "pack already requested, try again later"}
	ErrAlreadyDownloaded = &XDCCDownloadError{Kind: "already_downloaded", Message: "file already downloaded"}
	ErrBotDenied         = &XDCCDownloadError{Kind: "bot_denied", Message: "bot denied the XDCC request"}
	ErrServerUnreachable = &XDCCDownloadError{Kind: "server_unreachable", Message: "IRC server is unreachable"}
	ErrUnrecoverable     = &XDCCDownloadError{Kind: "unrecoverable", Message: "unrecoverable error (IP banned?)"}
	ErrDownloadFailed    = &XDCCDownloadError{Kind: "download_failed", Message: "download did not complete"}
)

// ---------------------------------------------------------------------------
// Options and result types
// ---------------------------------------------------------------------------

// DownloadOptions configures a download session.
type DownloadOptions struct {
	ConnectTimeout   int    // seconds to wait for bot to initiate DCC (default 120)
	StallTimeout     int    // seconds of no transfer progress before aborting (0 = disabled, default 60)
	FallbackChannel  string // used if WHOIS finds no channels
	ThrottleBytes    int64  // bytes/sec limit, -1 = unlimited
	WaitTime         int    // seconds to wait before sending XDCC request
	Username         string // IRC nick to use; empty = random
	ChannelJoinDelay int    // seconds to wait after connecting before WHOIS; -1 = random 5-10
}

// PackResult holds the outcome of a single pack download.
type PackResult struct {
	FilePath      string // non-empty on success
	Error         error
	LastBotNotice string // last NOTICE from bot (useful when Error != nil)
}

// ---------------------------------------------------------------------------
// Client struct
// ---------------------------------------------------------------------------

// Client manages the download of one or more XDCC packs on a single IRC
// connection. Packs on the same server are downloaded without disconnecting;
// channels already joined are not rejoined.
type Client struct {
	packs     []*entities.XDCCPack
	opts      DownloadOptions
	verbosity int // 0=normal, 1=verbose, 2=debug, -1=quiet

	// IRC connection (reset on reconnect)
	irc            *girc.Client
	ircErrCh       chan error     // receives error from irc.Connect() goroutine
	connectedCh    chan struct{}  // closed on CONNECTED event
	joinedChannels map[string]bool // channels joined in this connection (cleared on reconnect)
	connectTime    time.Time

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
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewClient creates a new XDCC Client that will download all packs in order.
// packs must all belong to the same IRC server.
// verbosity: -1=quiet, 0=normal, 1=verbose (-v), 2=debug (-vv).
func NewClient(packs []*entities.XDCCPack, opts DownloadOptions, verbosity int) *Client {
	if opts.ChannelJoinDelay < 0 {
		n, _ := rand.Int(rand.Reader, big.NewInt(6))
		opts.ChannelJoinDelay = int(n.Int64()) + 5
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = 120
	}
	if opts.StallTimeout < 0 {
		opts.StallTimeout = 0
	}
	return &Client{
		packs:     packs,
		opts:      opts,
		verbosity: verbosity,
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// DownloadAll downloads all packs sequentially, reusing the IRC connection
// for packs on the same server. Returns one PackResult per pack.
func (c *Client) DownloadAll() []PackResult {
	results := make([]PackResult, len(c.packs))

	if err := c.connect(); err != nil {
		for i := range results {
			results[i].Error = err
		}
		return results
	}

	for i := range c.packs {
		if i > 0 {
			c.debugf("Waiting 3s before next pack")
			time.Sleep(3 * time.Second)
		}
		results[i] = c.downloadPackAtIndex(i, 0)
		// Fatal errors: propagate to all remaining packs
		if results[i].Error != nil {
			if errors.Is(results[i].Error, ErrServerUnreachable) ||
				errors.Is(results[i].Error, ErrUnrecoverable) {
				for j := i + 1; j < len(results); j++ {
					results[j].Error = results[i].Error
				}
				break
			}
		}
	}

	c.irc.Close()
	// Drain ircErrCh so the goroutine can exit
	select {
	case <-c.ircErrCh:
	case <-time.After(5 * time.Second):
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
	server := c.packs[0].Server
	if err := c.checkServerReachable(server.Address); err != nil {
		return err
	}

	nick := c.opts.Username
	if nick == "" {
		nick = randomUsername()
	} else {
		nick = nick + randomSuffix(3)
	}
	c.infof("Connecting to %s:%d as '%s'", server.Address, server.Port, nick)

	c.connectedCh = make(chan struct{})
	c.joinedChannels = make(map[string]bool)
	c.ircErrCh = make(chan error, 1)

	c.irc = girc.New(girc.Config{
		Server:      server.Address,
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
	case err := <-c.ircErrCh:
		if err != nil {
			if isConnectError(err) {
				return fmt.Errorf("%w: %v", ErrServerUnreachable, err)
			}
			return err
		}
		return fmt.Errorf("IRC connection closed before CONNECTED event")
	case <-time.After(timeout):
		c.irc.Close()
		return ErrTimeout
	}
}

func (c *Client) reconnect() error {
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

	c.debugf("Pack: #%d from bot '%s'", pack.PackNumber, pack.Bot)

	// Channel-join delay only on first connection (not between packs)
	if idx == 0 {
		c.debugf("Waiting %ds before WHOIS (channel join delay)", c.opts.ChannelJoinDelay)
		time.Sleep(time.Duration(c.opts.ChannelJoinDelay) * time.Second)
	}

	c.debugf("Sending WHOIS for bot '%s'", pack.Bot)
	c.irc.Cmd.Whois(pack.Bot)

	err := c.waitForCurrentPack()
	if err == nil {
		return PackResult{FilePath: pack.GetFilepath()}
	}

	switch {
	case errors.Is(err, ErrPackAlreadyReq):
		fmt.Println("Pack already requested. Waiting 60 seconds before retrying...")
		time.Sleep(60 * time.Second)
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
	<-c.downloadDone
	return c.downloadError
}

// ---------------------------------------------------------------------------
// IRC handlers
// ---------------------------------------------------------------------------

func (c *Client) registerHandlers() {
	c.irc.Handlers.Add(girc.CONNECTED, func(client *girc.Client, e girc.Event) {
		c.connectTime = time.Now()
		c.infof("Connected to server")
		close(c.connectedCh)
	})

	// End of WHOIS: decide whether to send XDCC now or wait for JOIN.
	c.irc.Handlers.Add(girc.RPL_ENDOFWHOIS, func(client *girc.Client, e girc.Event) {
		c.debugf("End of WHOIS")
		if c.messageSent.Load() {
			return
		}
		if c.needsJoin.Load() {
			// We sent a JOIN; wait for the JOIN event to trigger XDCC.
			return
		}
		if c.whoisFoundChannels.Load() {
			// All channels were already joined — send XDCC directly.
			c.sendXDCCRequest(client)
			return
		}
		// No channels found in WHOIS at all.
		if c.opts.FallbackChannel != "" {
			ch := c.opts.FallbackChannel
			if !strings.HasPrefix(ch, "#") {
				ch = "#" + ch
			}
			c.debugf("No channels from WHOIS; joining fallback channel %s", ch)
			c.needsJoin.Store(true)
			client.Cmd.Join(ch)
		} else {
			c.debugf("No channels from WHOIS and no fallback; sending XDCC request directly")
			c.sendXDCCRequest(client)
		}
	})

	// WHOIS channels: join only channels we have not yet joined.
	c.irc.Handlers.Add(girc.RPL_WHOISCHANNELS, func(client *girc.Client, e girc.Event) {
		if len(e.Params) < 2 {
			return
		}
		c.logf("WHOIS channels: %s", e.Params[len(e.Params)-1])
		rawChannels := e.Params[len(e.Params)-1]
		for _, part := range strings.Fields(rawChannels) {
			part = strings.TrimLeft(part, "@+%&~")
			if !strings.HasPrefix(part, "#") {
				continue
			}
			ch := strings.ToLower(part)
			c.whoisFoundChannels.Store(true)
			c.mu.Lock()
			alreadyIn := c.joinedChannels[ch]
			c.mu.Unlock()
			if alreadyIn {
				c.logf("Already in channel %s, skipping JOIN", part)
			} else {
				c.logf("Joining channel %s", part)
				c.needsJoin.Store(true)
				time.Sleep(time.Duration(1+randN(2)) * time.Second)
				client.Cmd.Join(part)
			}
		}
	})

	// JOIN: record membership, send XDCC if pending.
	c.irc.Handlers.Add(girc.JOIN, func(client *girc.Client, e girc.Event) {
		if e.Source == nil || !strings.EqualFold(e.Source.Name, client.GetNick()) {
			return
		}
		ch := strings.ToLower(e.Params[0])
		c.mu.Lock()
		c.joinedChannels[ch] = true
		c.mu.Unlock()
		c.debugf("Joined channel: %s", e.Params[0])
		if !c.messageSent.Load() {
			c.sendXDCCRequest(client)
		}
	})

	// CTCP DCC handler (DCC SEND / DCC ACCEPT for resume).
	c.irc.CTCP.Set("DCC", func(client *girc.Client, ctcp girc.CTCPEvent) {
		sourceHost := ""
		if ctcp.Source != nil {
			sourceHost = ctcp.Source.Host
		}
		c.handleDCC(client, ctcp.Text, sourceHost)
	})

	// NOTICE from bot.
	c.irc.Handlers.Add(girc.NOTICE, func(client *girc.Client, e girc.Event) {
		notice := e.Last()
		msg := strings.ToLower(notice)
		// These are standard IRC server ident/hostname check messages — suppress in quiet mode.
		quietFiltered := []string{
			"looking up your hostname",
			"checking ident",
			"couldn't resolve your hostname",
			"no ident response",
		}
		isQuietFiltered := false
		for _, f := range quietFiltered {
			if strings.Contains(msg, f) {
				isQuietFiltered = true
				break
			}
		}
		if isQuietFiltered {
			c.logf("Bot notice: %s", notice)
		} else {
			c.noticef("Bot notice: %s", notice)
		}

		alreadyReqMsgs := []string{"you already requested", "richiesto questo pack!"}
		blockedMsgs := []string{"xdcc send negato", "numero pack errato", "invalid pack number",
			"gli slots sono occupati", "denied"}

		for _, s := range alreadyReqMsgs {
			if strings.Contains(msg, s) {
				c.mu.Lock()
				c.lastBotNotice = notice
				c.mu.Unlock()
				c.finishWithError(ErrPackAlreadyReq)
				return
			}
		}
		for _, s := range blockedMsgs {
			if strings.Contains(msg, s) {
				c.mu.Lock()
				c.lastBotNotice = notice
				c.mu.Unlock()
				c.finishWithError(ErrBotDenied)
				return
			}
		}
	})

	c.irc.Handlers.Add(girc.ERR_NOSUCHNICK, func(client *girc.Client, e girc.Event) {
		c.noticef("Bot '%s' not found on server", c.currentPack().Bot)
		c.finishWithError(ErrBotNotFound)
	})

	c.irc.Handlers.Add(girc.ERROR, func(client *girc.Client, e girc.Event) {
		c.noticef("IRC error: %s", e.Last())
		c.finishWithError(ErrUnrecoverable)
	})
}

func (c *Client) sendXDCCRequest(client *girc.Client) {
	if c.messageSent.Swap(true) {
		return
	}
	if c.opts.WaitTime > 0 {
		c.logf("Waiting %ds before sending XDCC request", c.opts.WaitTime)
		time.Sleep(time.Duration(c.opts.WaitTime) * time.Second)
	}
	pack := c.currentPack()
	msg := pack.GetRequestMessage(false)
	c.debugf("Sending XDCC request: /msg %s %s", pack.Bot, msg)
	client.Cmd.Message(pack.Bot, msg)
}

// ---------------------------------------------------------------------------
// DCC handling
// ---------------------------------------------------------------------------

func (c *Client) handleDCC(client *girc.Client, text string, sourceHost string) {
	parts := splitDCC(text)
	if len(parts) == 0 {
		return
	}
	switch strings.ToUpper(parts[0]) {
	case "SEND":
		c.handleDCCSend(client, parts, sourceHost)
	case "ACCEPT":
		c.handleDCCAccept(parts)
	default:
		c.logf("Unknown DCC command: %s", parts[0])
	}
}

func (c *Client) handleDCCSend(client *girc.Client, parts []string, sourceHost string) {
	if len(parts) < 5 {
		c.logf("Malformed DCC SEND: %v", parts)
		return
	}
	filename := parts[1]
	ipNum := parts[2]
	port := parts[3]
	sizeStr := parts[4]

	peerIP := ipNumToQuad(ipNum)
	if peerIP == "0.0.0.0" {
		if sourceHost != "" {
			c.logf("Passive DCC: using source host %s instead of 0.0.0.0", sourceHost)
			peerIP = sourceHost
		} else {
			peerIP = c.currentPack().Server.Address
			c.logf("Passive DCC with unknown source host, falling back to %s", peerIP)
		}
	}
	peerAddr := peerIP + ":" + port
	filesize := parseI64(sizeStr)

	pack := c.currentPack()
	pack.SetFilename(filename, false)
	c.filesize = filesize

	c.mu.Lock()
	c.peerAddr = peerAddr
	c.mu.Unlock()

	c.debugf("DCC SEND: file=%s addr=%s size=%s", filename, peerAddr, entities.HumanReadableBytes(filesize))

	existingPath := pack.GetFilepath()
	c.debugf("Checking for existing file at: %s", existingPath)
	if fi, err := os.Stat(existingPath); err == nil {
		pos := fi.Size()
		c.logf("Existing file: %s, remote: %s",
			entities.HumanReadableBytes(pos), entities.HumanReadableBytes(filesize))
		if pos >= filesize {
			c.noticef("File already fully downloaded (local: %s >= remote: %s), skipping",
				entities.HumanReadableBytes(pos), entities.HumanReadableBytes(filesize))
			c.finishWithError(ErrAlreadyDownloaded)
			return
		}
		c.progress = pos
		resumeParam := fmt.Sprintf("\"%s\" %s %d", filename, port, pos)
		c.debugf("Resuming download from %s / %s",
			entities.HumanReadableBytes(pos), entities.HumanReadableBytes(filesize))
		c.logf("Sending DCC RESUME: %s", resumeParam)
		client.Cmd.SendCTCP(pack.Bot, "DCC", "RESUME "+resumeParam)
		return
	}

	c.startDownload(peerAddr, false)
}

func (c *Client) handleDCCAccept(parts []string) {
	if len(parts) < 4 {
		return
	}
	c.debugf("DCC ACCEPT: resuming download")
	c.startDownloadAppend()
}

func (c *Client) startDownload(addr string, appendMode bool) {
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendMode {
		flag = os.O_APPEND | os.O_WRONLY
	}

	path := c.currentPack().GetFilepath()
	f, err := os.OpenFile(path, flag, 0644)
	if err != nil {
		c.finishWithError(fmt.Errorf("cannot open file: %w", err))
		return
	}

	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		f.Close()
		c.finishWithError(fmt.Errorf("DCC connection failed: %w", err))
		return
	}

	c.mu.Lock()
	c.dccFile = f
	c.dccConn = conn
	c.downStartTime = time.Now()
	c.dccTimestamp = time.Now()
	c.downloading = true
	c.mu.Unlock()

	c.debugf("Starting download (append=%v) to %s", appendMode, path)
	c.infof("Downloading %s → %s", entities.HumanReadableBytes(c.filesize), path)

	c.startOnce.Do(func() { close(c.downloadStarted) })
	c.lastActivity.Store(time.Now().UnixNano())

	go c.ackSender()
	go c.progressPrinter()
	go c.receiveData()
}

func (c *Client) startDownloadAppend() {
	c.mu.Lock()
	peerAddr := c.peerAddr
	c.mu.Unlock()
	if peerAddr == "" {
		c.finishWithError(ErrDownloadFailed)
		return
	}
	c.startDownload(peerAddr, true)
}

func (c *Client) receiveData() {
	defer func() {
		c.mu.Lock()
		c.downloading = false
		if c.dccFile != nil {
			c.dccFile.Close()
		}
		c.mu.Unlock()

		if c.progress >= c.filesize {
			c.logf("Download complete")
			c.finishSuccess()
		} else {
			c.logf("Download incomplete: got %d of %d bytes", c.progress, c.filesize)
			c.finishWithError(ErrDownloadFailed)
		}
	}()

	buf := make([]byte, 4096)
	for {
		n, err := c.dccConn.Read(buf)
		if n > 0 {
			c.mu.Lock()
			_, werr := c.dccFile.Write(buf[:n])
			c.mu.Unlock()
			if werr != nil {
				c.logf("Write error: %v", werr)
				return
			}
			atomic.AddInt64(&c.progress, int64(n))
			c.lastActivity.Store(time.Now().UnixNano())

			if c.opts.ThrottleBytes > 0 {
				c.mu.Lock()
				delta := time.Since(c.dccTimestamp).Seconds()
				chunkTime := float64(n) / float64(c.opts.ThrottleBytes)
				sleepTime := chunkTime - delta
				c.dccTimestamp = time.Now()
				c.mu.Unlock()
				if sleepTime > 0 {
					time.Sleep(time.Duration(sleepTime * float64(time.Second)))
				}
			}
			c.enqueueACK()
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) ackSender() {
	for ack := range c.ackQueue {
		c.mu.Lock()
		conn := c.dccConn
		c.mu.Unlock()
		if conn == nil {
			continue
		}
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		conn.Write(ack)
	}
}

func (c *Client) enqueueACK() {
	prog := atomic.LoadInt64(&c.progress)
	var ack []byte
	if prog <= 0xFFFFFFFF {
		ack = make([]byte, 4)
		binary.BigEndian.PutUint32(ack, uint32(prog))
	} else {
		ack = make([]byte, 8)
		binary.BigEndian.PutUint64(ack, uint64(prog))
	}
	select {
	case c.ackQueue <- ack:
	default:
	}
}

func (c *Client) progressPrinter() {
	c.mu.Lock()
	for !c.downloading {
		c.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		c.mu.Lock()
	}
	c.mu.Unlock()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastProgress int64
	lastTime := time.Now()

	for {
		select {
		case <-ticker.C:
			prog := atomic.LoadInt64(&c.progress)
			total := c.filesize
			elapsed := time.Since(lastTime).Seconds()
			speed := float64(prog-lastProgress) / elapsed
			lastProgress = prog
			lastTime = time.Now()

			pct := 0.0
			if total > 0 {
				pct = float64(prog) / float64(total) * 100
			}

			eta := ""
			if speed > 0 && total > prog {
				remaining := time.Duration(float64(total-prog)/speed) * time.Second
				if remaining < 90*time.Second {
					eta = fmt.Sprintf(" remaining: %ds", int(remaining.Seconds()))
				} else {
					eta = fmt.Sprintf(" remaining: %dm %ds",
						int(remaining.Minutes()), int(remaining.Seconds())%60)
				}
			}

			speedKB := speed / 1024
			speedStr := fmt.Sprintf("%.1f KB/s", speedKB)
			if speedKB >= 1024 {
				speedStr = fmt.Sprintf("%.2f MB/s", speedKB/1024)
			}

			fmt.Printf("\r  %.1f%% [%s / %s] %s%s    ",
				pct,
				entities.HumanReadableBytes(prog),
				entities.HumanReadableBytes(total),
				speedStr,
				eta)

			c.mu.Lock()
			dl := c.downloading
			c.mu.Unlock()
			if !dl {
				fmt.Println()
				return
			}
		case <-c.downloadDone:
			fmt.Println()
			return
		}
	}
}

// stallWatcher monitors transfer progress. On stall it closes the DCC
// connection (not the IRC connection) so the download can be retried.
func (c *Client) stallWatcher() {
	stall := time.Duration(c.opts.StallTimeout) * time.Second
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.downloadDone:
			return
		case <-ticker.C:
			last := c.lastActivity.Load()
			if last == 0 {
				continue
			}
			idle := time.Since(time.Unix(0, last))
			if idle >= stall {
				c.noticef("Transfer stalled for %s (no data received), aborting",
					idle.Round(time.Second))
				c.mu.Lock()
				if c.dccConn != nil {
					c.dccConn.Close()
				}
				c.mu.Unlock()
				c.finishWithError(ErrTimeout)
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Finish helpers
// ---------------------------------------------------------------------------

// finishSuccess records a successful download. Does NOT close the IRC
// connection so subsequent packs can reuse it.
func (c *Client) finishSuccess() {
	elapsed := time.Since(c.downStartTime)
	speed := float64(c.filesize) / elapsed.Seconds() / 1024
	fmt.Printf("File %s downloaded successfully in %s at %.1f KB/s\n",
		c.currentPack().Filename,
		formatDuration(elapsed),
		speed)
	c.closeOnce.Do(func() {
		close(c.downloadDone)
		close(c.ackQueue)
	})
}

// finishWithError records a download error. Does NOT close the IRC
// connection so the session can retry or continue with the next pack.
func (c *Client) finishWithError(err error) {
	c.mu.Lock()
	if c.downloadError == nil {
		c.downloadError = err
	}
	c.mu.Unlock()
	c.closeOnce.Do(func() {
		close(c.downloadDone)
		close(c.ackQueue)
	})
}

// ---------------------------------------------------------------------------
// Server reachability
// ---------------------------------------------------------------------------

func (c *Client) checkServerReachable(host string) error {
	addrs, err := net.LookupHost(host)
	if err != nil {
		c.noticef("DNS resolution failed for %s: %v", host, err)
		return fmt.Errorf("%w: cannot resolve %s: %v", ErrServerUnreachable, host, err)
	}
	c.debugf("DNS resolved %s → %v", host, addrs)
	for _, addr := range addrs {
		if addr == "0.0.0.0" || addr == "::" {
			c.noticef("Server %s resolves to %s — DNS-blocked or server is down", host, addr)
			return fmt.Errorf("%w: %s resolves to %s (DNS-blocked or server down)",
				ErrServerUnreachable, host, addr)
		}
	}
	return nil
}

func isConnectError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, k := range []string{
		"connection refused", "no route to host", "network is unreachable",
		"i/o timeout", "no such host", "dial ",
	} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Logging
// ---------------------------------------------------------------------------

func (c *Client) infof(format string, args ...interface{}) {
	if c.verbosity >= 0 {
		log.Printf("[xdcc] "+format, args...)
	}
}

// noticef prints at verbosity >= -1 (quiet and above).
// Use for errors, bot messages, and status that matter even in quiet mode.
func (c *Client) noticef(format string, args ...interface{}) {
	if c.verbosity >= -1 {
		log.Printf("[xdcc] "+format, args...)
	}
}

func (c *Client) logf(format string, args ...interface{}) {
	if c.verbosity >= 1 {
		log.Printf("[xdcc] "+format, args...)
	}
}

func (c *Client) debugf(format string, args ...interface{}) {
	if c.verbosity >= 2 {
		log.Printf("[xdcc] "+format, args...)
	}
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func randomUsername() string {
	firstNames := []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank", "Grace", "Hank"}
	lastNames := []string{"Smith", "Jones", "Brown", "Wilson", "Taylor", "Davis", "Clark", "Lewis"}
	n1, _ := rand.Int(rand.Reader, big.NewInt(int64(len(firstNames))))
	n2, _ := rand.Int(rand.Reader, big.NewInt(int64(len(lastNames))))
	num, _ := rand.Int(rand.Reader, big.NewInt(90))
	return fmt.Sprintf("%s%s%d%s",
		firstNames[n1.Int64()], lastNames[n2.Int64()], num.Int64()+10, randomSuffix(3))
}

func randomSuffix(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[idx.Int64()]
	}
	return string(b)
}

func formatDuration(d time.Duration) string {
	if d < 90*time.Second {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}

func ipNumToQuad(ipNum string) string {
	n := parseU32(ipNum)
	return fmt.Sprintf("%d.%d.%d.%d",
		(n>>24)&0xFF, (n>>16)&0xFF, (n>>8)&0xFF, n&0xFF)
}

func parseI64(s string) int64 {
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v
}

func parseU32(s string) uint32 {
	var v uint32
	fmt.Sscanf(s, "%d", &v)
	return v
}

func randN(n int) int {
	r, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(r.Int64())
}

// splitDCC splits a DCC message text, respecting quoted filenames.
func splitDCC(s string) []string {
	var parts []string
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		if s[0] == '"' {
			end := strings.Index(s[1:], "\"")
			if end < 0 {
				parts = append(parts, s[1:])
				break
			}
			parts = append(parts, s[1:end+1])
			s = strings.TrimSpace(s[end+2:])
		} else {
			sp := strings.IndexByte(s, ' ')
			if sp < 0 {
				parts = append(parts, s)
				break
			}
			parts = append(parts, s[:sp])
			s = strings.TrimSpace(s[sp+1:])
		}
	}
	return parts
}
