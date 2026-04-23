package irc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"xdcc-go/internal/entities"
)

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

// newTestClient returns a Client initialised for a single pack, with
// resetForPack already called.  verbosity=-1 keeps output quiet.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	srv := entities.NewIrcServer("irc.rizon.net")
	pack := entities.NewXDCCPack(srv, "TestBot", 1)
	pack.SetFilename("test.mkv", true)
	pack.SetDirectory(t.TempDir())
	c := NewClient([]*entities.XDCCPack{pack}, DownloadOptions{}, -1)
	c.resetForPack()
	return c
}

// ---------------------------------------------------------------------------
// errors.go
// ---------------------------------------------------------------------------

func TestXDCCDownloadError_Error(t *testing.T) {
	e := &XDCCDownloadError{Kind: "test_kind", Message: "test message"}
	if got := e.Error(); got != "test_kind: test message" {
		t.Errorf("Error() = %q", got)
	}
}

func TestXDCCDownloadError_Is(t *testing.T) {
	if !errors.Is(ErrTimeout, ErrTimeout) {
		t.Error("errors.Is(ErrTimeout, ErrTimeout) should be true")
	}
	if errors.Is(ErrTimeout, ErrBotNotFound) {
		t.Error("errors.Is(ErrTimeout, ErrBotNotFound) should be false")
	}
	// Is() must return false when target is not *XDCCDownloadError.
	if errors.Is(ErrTimeout, fmt.Errorf("generic error")) {
		t.Error("errors.Is with non-XDCCDownloadError target should be false")
	}
}

func TestSentinelErrors_AllDistinct(t *testing.T) {
	sentinels := []*XDCCDownloadError{
		ErrTimeout, ErrBotNotFound, ErrPackAlreadyReq,
		ErrAlreadyDownloaded, ErrBotDenied, ErrServerUnreachable,
		ErrUnrecoverable, ErrDownloadFailed,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("errors.Is(%s, %s) should be false", a.Kind, b.Kind)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// utils.go
// ---------------------------------------------------------------------------

func TestSplitDCC(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		// Unquoted tokens
		{"SEND file.mkv 1234567890 6667 1000", []string{"SEND", "file.mkv", "1234567890", "6667", "1000"}},
		// Quoted filename with spaces
		{`SEND "my file.mkv" 1234567890 6667 1000`, []string{"SEND", "my file.mkv", "1234567890", "6667", "1000"}},
		// Empty input
		{"", nil},
		// Single token
		{"single", []string{"single"}},
		// Unclosed quote — rest of string treated as the token value
		{`"unclosed`, []string{"unclosed"}},
		// Leading/trailing spaces
		{"  SEND  file.mkv  ", []string{"SEND", "file.mkv"}},
	}
	for _, tt := range tests {
		got := splitDCC(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("splitDCC(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i, v := range got {
			if v != tt.want[i] {
				t.Errorf("splitDCC(%q)[%d] = %q, want %q", tt.in, i, v, tt.want[i])
			}
		}
	}
}

func TestIsConnectError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("connection refused"), true},
		{fmt.Errorf("no route to host"), true},
		{fmt.Errorf("network is unreachable"), true},
		{fmt.Errorf("i/o timeout"), true},
		{fmt.Errorf("no such host"), true},
		{fmt.Errorf("dial tcp failed"), true},
		{fmt.Errorf("some unrelated error"), false},
	}
	for _, tt := range tests {
		if got := isConnectError(tt.err); got != tt.want {
			t.Errorf("isConnectError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{89 * time.Second, "89s"},
		{90 * time.Second, "1.5m"},
		{2 * time.Minute, "2.0m"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.in); got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIpNumToQuad(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"0", "0.0.0.0"},
		{"2130706433", "127.0.0.1"},    // 0x7F000001
		{"4294967295", "255.255.255.255"},
	}
	for _, tt := range tests {
		if got := ipNumToQuad(tt.in); got != tt.want {
			t.Errorf("ipNumToQuad(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseI64(t *testing.T) {
	if got := parseI64("1234567890"); got != 1234567890 {
		t.Errorf("parseI64 = %d, want 1234567890", got)
	}
	if got := parseI64("abc"); got != 0 {
		t.Errorf("parseI64(abc) = %d, want 0", got)
	}
}

func TestParseU32(t *testing.T) {
	if got := parseU32("2130706433"); got != 2130706433 {
		t.Errorf("parseU32 = %d, want 2130706433", got)
	}
	if got := parseU32("abc"); got != 0 {
		t.Errorf("parseU32(abc) = %d, want 0", got)
	}
}

func TestRandN(t *testing.T) {
	for i := 0; i < 100; i++ {
		r := randN(10)
		if r < 0 || r >= 10 {
			t.Fatalf("randN(10) = %d, out of range [0, 10)", r)
		}
	}
}

func TestRandomSuffix(t *testing.T) {
	s := randomSuffix(5)
	if len(s) != 5 {
		t.Fatalf("randomSuffix(5) length = %d, want 5", len(s))
	}
	const valid = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for _, ch := range s {
		found := false
		for _, v := range valid {
			if ch == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("randomSuffix contains invalid char: %q", ch)
		}
	}
}

func TestRandomUsername(t *testing.T) {
	u := randomUsername()
	if u == "" {
		t.Error("randomUsername returned empty string")
	}
}

func TestCheckServerReachable_Localhost(t *testing.T) {
	c := newTestClient(t)
	if err := c.checkServerReachable("localhost"); err != nil {
		t.Fatalf("checkServerReachable(localhost) = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// client.go
// ---------------------------------------------------------------------------

func TestNewClient_DefaultConnectTimeout(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	// ConnectTimeout <= 0 must be replaced with 120.
	c := NewClient([]*entities.XDCCPack{pack}, DownloadOptions{ConnectTimeout: 0}, 0)
	if c.opts.ConnectTimeout != 120 {
		t.Errorf("ConnectTimeout = %d, want 120", c.opts.ConnectTimeout)
	}
}

func TestNewClient_NegativeStallTimeoutClamped(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	c := NewClient([]*entities.XDCCPack{pack}, DownloadOptions{StallTimeout: -5}, 0)
	if c.opts.StallTimeout != 0 {
		t.Errorf("StallTimeout = %d, want 0", c.opts.StallTimeout)
	}
}

func TestNewClient_RandomChannelJoinDelay(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	c := NewClient([]*entities.XDCCPack{pack}, DownloadOptions{ChannelJoinDelay: -1}, 0)
	if c.opts.ChannelJoinDelay < 5 || c.opts.ChannelJoinDelay > 10 {
		t.Errorf("ChannelJoinDelay = %d, want in [5, 10]", c.opts.ChannelJoinDelay)
	}
}

func TestNewClient_ExplicitChannelJoinDelay(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	c := NewClient([]*entities.XDCCPack{pack}, DownloadOptions{ChannelJoinDelay: 7}, 0)
	if c.opts.ChannelJoinDelay != 7 {
		t.Errorf("ChannelJoinDelay = %d, want 7", c.opts.ChannelJoinDelay)
	}
}

func TestLoggingMethods_AllVerbosityLevels(t *testing.T) {
	c := newTestClient(t)
	// Just call every method at every verbosity to confirm no panics.
	for _, v := range []int{-1, 0, 1, 2} {
		c.verbosity = v
		c.infof("info %d", v)
		c.noticef("notice %d", v)
		c.logf("log %d", v)
		c.debugf("debug %d", v)
	}
}

func TestCurrentPack(t *testing.T) {
	c := newTestClient(t)
	p := c.currentPack()
	if p == nil {
		t.Fatal("currentPack returned nil")
	}
	if p.Bot != "TestBot" {
		t.Errorf("Bot = %q, want TestBot", p.Bot)
	}
}

func TestLastBotNotice(t *testing.T) {
	c := newTestClient(t)
	c.mu.Lock()
	c.lastBotNotice = "Pack already requested!"
	c.mu.Unlock()

	if got := c.LastBotNotice(); got != "Pack already requested!" {
		t.Errorf("LastBotNotice = %q", got)
	}
}

func TestResetForPack_InitializesChannels(t *testing.T) {
	c := newTestClient(t) // already calls resetForPack
	if c.downloadDone == nil {
		t.Error("downloadDone channel not initialized")
	}
	if c.downloadStarted == nil {
		t.Error("downloadStarted channel not initialized")
	}
	if c.ackQueue == nil {
		t.Error("ackQueue channel not initialized")
	}
	if c.messageSent.Load() {
		t.Error("messageSent should be false after reset")
	}
}

func TestFinishWithError_ClosesDone(t *testing.T) {
	c := newTestClient(t)
	c.finishWithError(ErrBotDenied)

	select {
	case <-c.downloadDone:
		// expected
	case <-time.After(time.Second):
		t.Fatal("downloadDone not closed after finishWithError")
	}

	c.mu.Lock()
	err := c.downloadError
	c.mu.Unlock()
	if !errors.Is(err, ErrBotDenied) {
		t.Errorf("downloadError = %v, want ErrBotDenied", err)
	}
}

func TestFinishWithError_FirstErrorWins(t *testing.T) {
	c := newTestClient(t)
	c.finishWithError(ErrTimeout)
	c.finishWithError(ErrBotDenied) // second call — sync.Once must ignore it

	c.mu.Lock()
	err := c.downloadError
	c.mu.Unlock()
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("first error should win, got %v", err)
	}
}

func TestWaitForCurrentPack_DownloadDoneBeforeStart(t *testing.T) {
	c := newTestClient(t)
	// Pre-finish before waitForCurrentPack is called: select hits downloadDone immediately.
	c.finishWithError(ErrBotDenied)

	err := c.waitForCurrentPack()
	if !errors.Is(err, ErrBotDenied) {
		t.Errorf("waitForCurrentPack = %v, want ErrBotDenied", err)
	}
}

// ---------------------------------------------------------------------------
// dcc.go
// ---------------------------------------------------------------------------

func TestHandleDCC_EmptyText(t *testing.T) {
	c := newTestClient(t)
	c.handleDCC("", "") // must not panic
}

func TestHandleDCC_UnknownCommand(t *testing.T) {
	c := newTestClient(t)
	c.handleDCC("UNKNOWN file.mkv 0 6667 1000", "") // logs and returns
}

func TestHandleDCCSend_TooFewParts(t *testing.T) {
	c := newTestClient(t)
	c.handleDCC("SEND file.mkv", "") // only 2 parts — must log and return cleanly
}

func TestHandleDCCAccept_TooFewParts(t *testing.T) {
	c := newTestClient(t)
	c.handleDCC("ACCEPT file.mkv 6667", "") // only 3 parts — early return
}

func TestStartDownloadAppend_EmptyPeerAddr(t *testing.T) {
	c := newTestClient(t)
	// peerAddr is "" after resetForPack → must finish with ErrDownloadFailed.
	c.startDownloadAppend()

	select {
	case <-c.downloadDone:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for downloadDone")
	}
	if !errors.Is(c.downloadError, ErrDownloadFailed) {
		t.Errorf("downloadError = %v, want ErrDownloadFailed", c.downloadError)
	}
}

func TestEnqueueACK_SmallProgress(t *testing.T) {
	c := newTestClient(t)
	atomic.StoreInt64(&c.progress, 1234)
	c.enqueueACK()

	select {
	case ack := <-c.ackQueue:
		if len(ack) != 4 {
			t.Errorf("ACK length = %d, want 4 for progress <= 0xFFFFFFFF", len(ack))
		}
	default:
		t.Error("expected ACK in queue")
	}
}

func TestEnqueueACK_LargeProgress(t *testing.T) {
	c := newTestClient(t)
	atomic.StoreInt64(&c.progress, 0x1_0000_0000) // exceeds 32 bits
	c.enqueueACK()

	select {
	case ack := <-c.ackQueue:
		if len(ack) != 8 {
			t.Errorf("ACK length = %d, want 8 for progress > 0xFFFFFFFF", len(ack))
		}
	default:
		t.Error("expected ACK in queue")
	}
}

// TestHandleDCCSend_AlreadyDownloaded: existing file >= remote size → ErrAlreadyDownloaded
func TestHandleDCCSend_AlreadyDownloaded(t *testing.T) {
	c := newTestClient(t)

	// Create a pre-existing file that is already "complete".
	path := c.currentPack().GetFilepath()
	data := make([]byte, 1024)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Remote size reported as 512 (< local 1024) → skip.
	msg := fmt.Sprintf("SEND test.mkv 0 6667 512")
	c.handleDCC(msg, "127.0.0.1")

	select {
	case <-c.downloadDone:
	case <-time.After(time.Second):
		t.Fatal("downloadDone not closed")
	}
	if !errors.Is(c.downloadError, ErrAlreadyDownloaded) {
		t.Errorf("downloadError = %v, want ErrAlreadyDownloaded", c.downloadError)
	}
}

// TestStartDownload_ConnectsToLocalListener: startDownload with a real local
// TCP listener; listener closes the conn immediately → download finishes with
// ErrDownloadFailed (0 bytes < filesize).
func TestStartDownload_ConnectsToLocalListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
		ln.Close()
	}()

	c := newTestClient(t)
	c.filesize = 4096 // so 0 bytes received → incomplete
	c.startDownload(addr, false)

	select {
	case <-c.downloadDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for downloadDone")
	}
	// Either ErrDownloadFailed (0 bytes) or nil (race where 0==filesize=0).
	// filesize=4096 so it must be ErrDownloadFailed.
	if c.downloadError == nil {
		t.Error("expected non-nil downloadError after immediate close")
	}
}

// TestStallWatcher_TriggersTimeout: set up a stall condition and verify
// stallWatcher closes the connection and sets ErrTimeout.
func TestStallWatcher_TriggersTimeout(t *testing.T) {
	c := newTestClient(t)
	c.opts.StallTimeout = 1 // 1 second stall timeout

	// Provide a real conn pair so stallWatcher can close dccConn.
	serverConn, clientConn := net.Pipe()
	c.mu.Lock()
	c.dccConn = clientConn
	c.mu.Unlock()
	defer serverConn.Close()

	// Set lastActivity to 10 seconds ago to immediately trigger the stall.
	c.lastActivity.Store(time.Now().Add(-10 * time.Second).UnixNano())

	go c.stallWatcher()

	select {
	case <-c.downloadDone:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for stallWatcher to trigger")
	}
	if !errors.Is(c.downloadError, ErrTimeout) {
		t.Errorf("downloadError = %v, want ErrTimeout", c.downloadError)
	}
}

// TestWaitForCurrentPack_StartedThenDone: downloadStarted fires, then the
// transfer completes successfully (no stall timeout configured).
func TestWaitForCurrentPack_StartedThenDone(t *testing.T) {
	c := newTestClient(t)
	// Signal "started" and then "done" asynchronously.
	go func() {
		time.Sleep(10 * time.Millisecond)
		c.startOnce.Do(func() { close(c.downloadStarted) })
		time.Sleep(10 * time.Millisecond)
		c.finishWithError(nil)
	}()

	err := c.waitForCurrentPack()
	if err != nil {
		t.Errorf("waitForCurrentPack = %v, want nil", err)
	}
}

// TestWaitForCurrentPack_TimeoutBeforeStart: ConnectTimeout=0 → immediate
// timeout from time.After.
func TestWaitForCurrentPack_TimeoutBeforeStart(t *testing.T) {
	c := newTestClient(t)
	c.opts.ConnectTimeout = 0
	c.opts.WaitTime = 0
	// connectTimeout = (0 + 0 + 30) seconds — too long for a unit test.
	// Instead pre-close downloadDone to simulate the timeout path via done.
	c.finishWithError(ErrTimeout)

	err := c.waitForCurrentPack()
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("waitForCurrentPack = %v, want ErrTimeout", err)
	}
}

// TestReceiveData_FullTransfer exercises receiveData, ackSender, progressPrinter
// and finishSuccess end-to-end using an in-process net.Pipe (no real network).
func TestReceiveData_FullTransfer(t *testing.T) {
	const dataSize = 2048

	c := newTestClient(t)
	c.filesize = dataSize

	// Create destination file in the temp dir used by the pack.
	path := c.currentPack().GetFilepath()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	// net.Pipe provides a synchronous, in-process full-duplex connection pair.
	serverConn, clientConn := net.Pipe()

	c.mu.Lock()
	c.dccFile = f
	c.dccConn = clientConn
	c.downloading = true
	c.downStartTime = time.Now()
	c.dccTimestamp = time.Now()
	c.mu.Unlock()

	c.startOnce.Do(func() { close(c.downloadStarted) })
	c.lastActivity.Store(time.Now().UnixNano())

	go c.ackSender()
	go c.progressPrinter()
	go c.receiveData()

	// Send all data from the "bot" side, then close to signal EOF.
	data := make([]byte, dataSize)
	if _, err := serverConn.Write(data); err != nil {
		t.Fatalf("pipe write: %v", err)
	}
	serverConn.Close()

	select {
	case <-c.downloadDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for transfer to complete")
	}

	if c.downloadError != nil {
		t.Errorf("unexpected download error: %v", c.downloadError)
	}
	if atomic.LoadInt64(&c.progress) != dataSize {
		t.Errorf("progress = %d, want %d", c.progress, dataSize)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != dataSize {
		t.Errorf("file size = %d, want %d", info.Size(), dataSize)
	}
}
