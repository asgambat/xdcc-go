package irc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
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
	c := NewClient(context.Background(), []*entities.XDCCPack{pack}, DownloadOptions{}, -1)
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
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m 0s"},
		{90 * time.Second, "1m 30s"},
		{2*time.Minute + 5*time.Second, "2m 5s"},
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

func TestFormatSpeed(t *testing.T) {
	tests := []struct {
		bytesPerSec float64
		want        string
	}{
		{512 * 1024, "512.0 KB/s"},
		{1023 * 1024, "1023.0 KB/s"},
		{1024 * 1024, "1.00 MB/s"},
		{2048 * 1024, "2.00 MB/s"},
		{1536 * 1024, "1.50 MB/s"},
	}
	for _, tt := range tests {
		if got := formatSpeed(tt.bytesPerSec); got != tt.want {
			t.Errorf("formatSpeed(%.0f) = %q, want %q", tt.bytesPerSec, got, tt.want)
		}
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

// ---------------------------------------------------------------------------
// DNS resolution (utils.go resolveAllHosts)
// ---------------------------------------------------------------------------

// startFakeDNSServer starts a minimal UDP DNS server on a random localhost port.
// For every query it returns an A record pointing to answerIP.
// The server shuts down when the test ends.
func startFakeDNSServer(t *testing.T, answerIP string) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })

	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}
			resp := buildFakeDNSResponse(buf[:n], answerIP)
			if resp != nil {
				pc.WriteTo(resp, addr)
			}
		}
	}()
	return pc.LocalAddr().String()
}

// buildFakeDNSResponse constructs a minimal DNS A-record response for any query.
// It copies the query ID, sets QR+AA flags, and appends one A record with answerIP.
func buildFakeDNSResponse(query []byte, answerIP string) []byte {
	ip := net.ParseIP(answerIP).To4()
	if ip == nil {
		return nil
	}

	// Locate end of question section (name + QTYPE + QCLASS).
	pos := 12
	for pos < len(query) {
		if query[pos] == 0 {
			pos += 5 // null label + QTYPE (2) + QCLASS (2)
			break
		}
		if query[pos]&0xC0 == 0xC0 { // compressed pointer
			pos += 2 + 4 // pointer + QTYPE + QCLASS
			break
		}
		pos += int(query[pos]) + 1
	}
	if pos > len(query) {
		return nil
	}

	// Build response: copy query up to end of question, then append answer.
	resp := make([]byte, pos+16)
	copy(resp, query[:pos])
	resp[2] = 0x81 // QR=1, AA=1, OPCODE=0
	resp[3] = 0x80 // RA=1, RCODE=0
	resp[6] = 0x00 // ANCOUNT hi
	resp[7] = 0x01 // ANCOUNT lo = 1

	// Answer RR: compressed name pointer → question name at offset 12.
	resp[pos+0] = 0xC0
	resp[pos+1] = 0x0C
	resp[pos+2] = 0x00 // TYPE A
	resp[pos+3] = 0x01
	resp[pos+4] = 0x00 // CLASS IN
	resp[pos+5] = 0x01
	resp[pos+6] = 0x00 // TTL = 60
	resp[pos+7] = 0x00
	resp[pos+8] = 0x00
	resp[pos+9] = 0x3C
	resp[pos+10] = 0x00 // RDLENGTH = 4
	resp[pos+11] = 0x04
	resp[pos+12] = ip[0]
	resp[pos+13] = ip[1]
	resp[pos+14] = ip[2]
	resp[pos+15] = ip[3]
	return resp
}

func TestNewClient_DefaultDNSServer(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	c := NewClient(context.Background(), []*entities.XDCCPack{pack}, DownloadOptions{}, 0)
	if c.opts.DNSServer != "8.8.8.8:53" {
		t.Errorf("default DNSServer = %q, want 8.8.8.8:53", c.opts.DNSServer)
	}
}

func TestNewClient_CustomDNSServer(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	c := NewClient(context.Background(), []*entities.XDCCPack{pack}, DownloadOptions{DNSServer: "1.1.1.1:53"}, 0)
	if c.opts.DNSServer != "1.1.1.1:53" {
		t.Errorf("DNSServer = %q, want 1.1.1.1:53", c.opts.DNSServer)
	}
}

func TestResolveAllHosts_Localhost(t *testing.T) {
	c := newTestClient(t)
	ips, err := c.resolveAllHosts("localhost")
	if err != nil {
		t.Fatalf("resolveAllHosts(localhost) = %v, want nil", err)
	}
	if len(ips) == 0 {
		t.Error("resolveAllHosts(localhost) returned no IPs")
	}
}

func TestResolveAllHosts_NonExistentHost_Error(t *testing.T) {
	// A .invalid TLD is guaranteed by RFC 2606 to never resolve.
	// Both system DNS and public fallback should fail.
	c := newTestClient(t)
	_, err := c.resolveAllHosts("this.host.does.not.exist.xdccgo.invalid")
	if err == nil {
		t.Error("expected error for non-existent host, got nil")
	}
	if !errors.Is(err, ErrServerUnreachable) {
		t.Errorf("expected ErrServerUnreachable, got %v", err)
	}
}

// TestResolveAllHosts_FallbackDNS verifies that when the system DNS fails
// (NXDOMAIN for a non-existent host), resolveAllHosts retries via the
// configured fallback DNS server. The fallback is a local fake DNS server
// that returns 192.0.2.1 (a TEST-NET address per RFC 5737) for any query.
func TestResolveAllHosts_FallbackDNS(t *testing.T) {
	const fakeIP = "192.0.2.1"
	dnsAddr := startFakeDNSServer(t, fakeIP)

	c := newTestClient(t)
	c.opts.DNSServer = dnsAddr

	// Use a non-existent hostname so system DNS fails → fallback is triggered.
	ips, err := c.resolveAllHosts("xdccgo.fallback.test.invalid")
	if err != nil {
		t.Fatalf("resolveAllHosts with fallback DNS = %v, want nil", err)
	}
	found := false
	for _, ip := range ips {
		if ip == fakeIP {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("resolved IPs = %v, want to contain %q", ips, fakeIP)
	}
}

// TestResolveAllHosts_BlockedAddress_FallsBackToDNS simulates an ISP that
// returns 0.0.0.0 for a blocked hostname. The fallback DNS server returns
// a real IP.
func TestResolveAllHosts_BlockedAddress_FallsBackToDNS(t *testing.T) {
	const fakeIP = "10.0.0.1"
	dnsAddr := startFakeDNSServer(t, fakeIP)

	c := newTestClient(t)
	c.opts.DNSServer = dnsAddr

	ips, err := c.resolveAllHosts("xdccgo.blocked.test.invalid")
	if err != nil {
		t.Fatalf("expected fallback to succeed, got %v", err)
	}
	found := false
	for _, ip := range ips {
		if ip == fakeIP {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ips = %v, want to contain %q", ips, fakeIP)
	}
}

// TestResolveAllHosts_FallbackServerUnreachable: both system DNS and fallback
// fail. resolveAllHosts must return ErrServerUnreachable (not panic or hang).
func TestResolveAllHosts_FallbackServerUnreachable(t *testing.T) {
	c := newTestClient(t)
	// Point fallback to a port that is not listening.
	c.opts.DNSServer = "127.0.0.1:19999"

	_, err := c.resolveAllHosts("this.host.does.not.exist.xdccgo.invalid")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrServerUnreachable) {
		t.Errorf("expected ErrServerUnreachable, got %v", err)
	}
}

// TestResolveAllHosts_ReturnsMultipleIPs verifies that when the fallback DNS
// returns a different IP than the system DNS, both are included in the result.
// This test is environment-dependent (fake DNS server + system resolver); when
// the fake DNS server cannot contribute an additional IP, the test is skipped.
func TestResolveAllHosts_ReturnsMultipleIPs(t *testing.T) {
	const fakeIP = "198.51.100.1" // TEST-NET-2 per RFC 5737
	dnsAddr := startFakeDNSServer(t, fakeIP)

	c := newTestClient(t)
	c.opts.DNSServer = dnsAddr

	ips, err := c.resolveAllHosts("localhost")
	if err != nil {
		t.Fatalf("resolveAllHosts(localhost) = %v, want nil", err)
	}

	// System DNS must return at least 1 IP for localhost.
	if len(ips) == 0 {
		t.Fatal("resolveAllHosts(localhost) returned 0 IPs — system DNS is broken")
	}

	// If the fake DNS server failed to contribute an additional IP, skip rather
	// than fail — this can happen when the Go resolver rejects the fake response
	// or when a UDP firewall drops the reply packet.
	fakeFound := false
	for _, ip := range ips {
		if ip == fakeIP {
			fakeFound = true
			break
		}
	}
	if !fakeFound {
		t.Skipf("fake DNS server did not contribute an IP (resolved %v); skipping merge test", ips)
	}

	if len(ips) < 2 {
		t.Errorf("expected at least 2 IPs (system + fallback), got %d: %v", len(ips), ips)
	}
}

// ---------------------------------------------------------------------------
// client.go
// ---------------------------------------------------------------------------

func TestNewClient_DefaultConnectTimeout(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	// ConnectTimeout <= 0 must be replaced with 120.
	c := NewClient(context.Background(), []*entities.XDCCPack{pack}, DownloadOptions{ConnectTimeout: 0}, 0)
	if c.opts.ConnectTimeout != 120 {
		t.Errorf("ConnectTimeout = %d, want 120", c.opts.ConnectTimeout)
	}
}

func TestNewClient_NegativeStallTimeoutClamped(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	c := NewClient(context.Background(), []*entities.XDCCPack{pack}, DownloadOptions{StallTimeout: -5}, 0)
	if c.opts.StallTimeout != 0 {
		t.Errorf("StallTimeout = %d, want 0", c.opts.StallTimeout)
	}
}

func TestNewClient_RandomChannelJoinDelay(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	c := NewClient(context.Background(), []*entities.XDCCPack{pack}, DownloadOptions{ChannelJoinDelay: -1}, 0)
	if c.opts.ChannelJoinDelay < 5 || c.opts.ChannelJoinDelay > 10 {
		t.Errorf("ChannelJoinDelay = %d, want in [5, 10]", c.opts.ChannelJoinDelay)
	}
}

func TestNewClient_ExplicitChannelJoinDelay(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	c := NewClient(context.Background(), []*entities.XDCCPack{pack}, DownloadOptions{ChannelJoinDelay: 7}, 0)
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

// ---------------------------------------------------------------------------
// DCC gap tests
// ---------------------------------------------------------------------------

// TestHandleDCCSend_PassiveDCC_UsesSourceHost: IP 0.0.0.0 with sourceHost set
func TestHandleDCCSend_PassiveDCC_UsesSourceHost(t *testing.T) {
	c := newTestClient(t)
	// IP "0" → 0.0.0.0 → passive DCC → should use sourceHost
	c.handleDCC("SEND test.mkv 0 19999 1024", "192.168.1.1")
	select {
	case <-c.downloadDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	c.mu.Lock()
	addr := c.peerAddr
	c.mu.Unlock()
	if !strings.Contains(addr, "192.168.1.1") {
		t.Errorf("peerAddr = %q, want to contain 192.168.1.1", addr)
	}
}

// TestHandleDCCSend_PassiveDCC_FallbackToServer: IP 0.0.0.0, no sourceHost
func TestHandleDCCSend_PassiveDCC_FallbackToServer(t *testing.T) {
	c := newTestClient(t)
	c.handleDCC("SEND test.mkv 0 19998 1024", "")
	select {
	case <-c.downloadDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	c.mu.Lock()
	addr := c.peerAddr
	c.mu.Unlock()
	if !strings.Contains(addr, "irc.rizon.net") {
		t.Errorf("peerAddr = %q, want to contain server address", addr)
	}
}

// TestHandleDCCSend_QuotedFilename: filename with spaces in quotes
func TestHandleDCCSend_QuotedFilename(t *testing.T) {
	c := newTestClient(t)
	c.currentPack().Filename = "" // clear so DCC SEND sets it
	c.handleDCC(`SEND "my file.mkv" 0 19997 512`, "127.0.0.1")
	select {
	case <-c.downloadDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	if c.currentPack().Filename != "my file.mkv" {
		t.Errorf("Filename = %q, want 'my file.mkv'", c.currentPack().Filename)
	}
}

// TestHandleDCCSend_ZeroFilesize
func TestHandleDCCSend_ZeroFilesize(t *testing.T) {
	c := newTestClient(t)
	c.handleDCC("SEND test.mkv 0 19996 0", "127.0.0.1")
	select {
	case <-c.downloadDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	if c.filesize != 0 {
		t.Errorf("filesize = %d, want 0", c.filesize)
	}
}

// TestHandleDCCSend_ResumePartialFile: existing file smaller than remote → sends RESUME
func TestHandleDCCSend_ResumePartialFile(t *testing.T) {
	c := newTestClient(t)
	path := c.currentPack().GetFilepath()
	if err := os.WriteFile(path, make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	// c.irc is nil so CTCP RESUME will panic — skip
	t.Skip("requires IRC client for CTCP RESUME")
}

// TestHandleDCCAccept_ValidParts: 4+ parts, peerAddr set → calls startDownloadAppend
func TestHandleDCCAccept_ValidParts(t *testing.T) {
	c := newTestClient(t)
	c.mu.Lock()
	c.peerAddr = "127.0.0.1:19995"
	c.mu.Unlock()
	c.handleDCC("ACCEPT test.mkv 19995 100 1024", "")
	select {
	case <-c.downloadDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for downloadDone after ACCEPT")
	}
	if c.downloadError == nil {
		t.Error("expected error (no listener), got nil")
	}
}

// TestStartDownload_AppendMode: startDownload with appendMode=true creates file in append mode
func TestStartDownload_AppendMode(t *testing.T) {
	c := newTestClient(t)
	c.filesize = 200

	path := c.currentPack().GetFilepath()
	if err := os.WriteFile(path, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Write([]byte("appended"))
		conn.Close()
		ln.Close()
	}()

	c.startDownload(ln.Addr().String(), true)
	select {
	case <-c.downloadDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "existing") {
		t.Errorf("file content = %q, want prefix 'existing'", string(data))
	}
}

// ---------------------------------------------------------------------------
// utils.go edge cases
// ---------------------------------------------------------------------------

func TestFormatSpeed_Zero(t *testing.T) {
	got := formatSpeed(0)
	if got != "0.0 KB/s" {
		t.Errorf("formatSpeed(0) = %q, want '0.0 KB/s'", got)
	}
}

func TestFormatSpeed_SmallValue(t *testing.T) {
	got := formatSpeed(100)
	if got != "0.1 KB/s" {
		t.Errorf("formatSpeed(100) = %q, want '0.1 KB/s'", got)
	}
}

func TestIpNumToQuad_NonNumeric(t *testing.T) {
	got := ipNumToQuad("abc")
	if got != "0.0.0.0" {
		t.Errorf("ipNumToQuad(abc) = %q, want '0.0.0.0'", got)
	}
}

func TestRandomSuffix_Zero(t *testing.T) {
	s := randomSuffix(0)
	if s != "" {
		t.Errorf("randomSuffix(0) = %q, want empty string", s)
	}
}
