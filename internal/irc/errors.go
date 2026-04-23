package irc

import "fmt"

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

// DownloadOptions configures a download session.
type DownloadOptions struct {
	ConnectTimeout   int    // seconds to wait for bot to initiate DCC (default 120)
	StallTimeout     int    // seconds of no transfer progress before aborting (0 = disabled, default 60)
	FallbackChannel  string // used if WHOIS finds no channels
	ThrottleBytes    int64  // bytes/sec limit, -1 = unlimited
	WaitTime         int    // seconds to wait before sending XDCC request
	Username         string // IRC nick to use; empty = random
	ChannelJoinDelay int    // seconds to wait after connecting before WHOIS; -1 = random 5-10
	// DNSServer is the fallback DNS resolver used when the system DNS returns a
	// blocked address (0.0.0.0 / ::). Format: "host:port". Default: "8.8.8.8:53".
	DNSServer string
}

// PackResult holds the outcome of a single pack download.
type PackResult struct {
	FilePath      string // non-empty on success
	Error         error
	LastBotNotice string // last NOTICE from bot (useful when Error != nil)
}
