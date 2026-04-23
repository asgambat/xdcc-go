package entities

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// IrcServer models an IRC server.
type IrcServer struct {
	Address string
	Port    int
}

// NewIrcServer creates a new IrcServer with the default port 6667.
func NewIrcServer(address string) IrcServer {
	return IrcServer{Address: address, Port: 6667}
}

// NewIrcServerWithPort creates a new IrcServer with a specific port.
func NewIrcServerWithPort(address string, port int) IrcServer {
	return IrcServer{Address: address, Port: port}
}

// ParseIrcServer parses a server string which may be "host", "host:port",
// "ip", or "ip:port". If no port is specified, defaults to 6667.
func ParseIrcServer(s string) IrcServer {
	// net.SplitHostPort handles IPv6 [::1]:port syntax too
	host, portStr, err := splitHostPort(s)
	if err != nil || portStr == "" {
		return IrcServer{Address: s, Port: 6667}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return IrcServer{Address: s, Port: 6667}
	}
	return IrcServer{Address: host, Port: port}
}

// splitHostPort splits "host:port" but also accepts bare "host" (no port).
func splitHostPort(s string) (host, port string, err error) {
	// If it contains "[" it's IPv6 — always use net.SplitHostPort
	if strings.Contains(s, "[") {
		host, port, err = net.SplitHostPort(s)
		return
	}
	// Count colons: 0 or 1 → hostname or host:port; >1 → bare IPv6
	colons := strings.Count(s, ":")
	if colons == 0 {
		return s, "", nil
	}
	if colons == 1 {
		host, port, err = net.SplitHostPort(s)
		return
	}
	// Multiple colons → bare IPv6 address (no port)
	return s, "", nil
}

// XDCCPack models an XDCC pack to be downloaded from an IRC bot.
type XDCCPack struct {
	Server           IrcServer
	Bot              string
	PackNumber       int
	Directory        string
	Filename         string
	OriginalFilename string
	Size             int64
}

// NewXDCCPack creates a new XDCCPack.
func NewXDCCPack(server IrcServer, bot string, packNumber int) *XDCCPack {
	return &XDCCPack{
		Server:     server,
		Bot:        bot,
		PackNumber: packNumber,
		Directory:  ".",
	}
}

// SetFilename sets or adjusts the filename.
// If a filename is already set and override is false, only the extension is updated.
func (p *XDCCPack) SetFilename(filename string, override bool) {
	if p.Filename != "" && !override {
		parts := strings.SplitN(filename, ".", 2)
		if len(parts) == 2 {
			ext := parts[1]
			if !strings.HasSuffix(p.Filename, "."+ext) {
				p.Filename += "." + ext
			}
		}
		return
	}
	p.Filename = filename
}

// SetOriginalFilename records the expected filename (used by search engines for validation).
func (p *XDCCPack) SetOriginalFilename(filename string) {
	p.OriginalFilename = filename
}

// SetDirectory sets the target download directory.
func (p *XDCCPack) SetDirectory(directory string) {
	p.Directory = directory
}

// SetSize sets the file size in bytes.
func (p *XDCCPack) SetSize(size int64) {
	p.Size = size
}

// IsFilenameValid checks if the provided filename matches the expected original filename.
func (p *XDCCPack) IsFilenameValid(filename string) bool {
	if p.OriginalFilename != "" {
		return filename == p.OriginalFilename
	}
	return true
}

// GetFilepath returns the full destination file path.
func (p *XDCCPack) GetFilepath() string {
	if p.Directory == "" || p.Directory == "." {
		return p.Filename
	}
	return p.Directory + "/" + p.Filename
}

// GetRequestMessage returns the XDCC send message for the bot.
// If full is true, includes "/msg <bot> " prefix.
func (p *XDCCPack) GetRequestMessage(full bool) string {
	msg := fmt.Sprintf("xdcc send #%d", p.PackNumber)
	if full {
		return fmt.Sprintf("/msg %s %s", p.Bot, msg)
	}
	return msg
}

// String returns a human-readable representation.
func (p *XDCCPack) String() string {
	return fmt.Sprintf("%s (/msg %s xdcc send #%d) [%s]",
		p.Filename, p.Bot, p.PackNumber, HumanReadableBytes(p.Size))
}

// HumanReadableBytes converts a byte count to a human-readable string.
func HumanReadableBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

var xdccMsgRegex = regexp.MustCompile(`^/msg [^ ]+ xdcc send #[0-9]+((,[0-9]+)*|(-[0-9]+(;[0-9]+)?)?)$`)

// ParseXDCCMessage parses an XDCC message and returns a list of XDCCPack objects.
// The message format is: /msg <bot> xdcc send #<pack>[,<pack>...] or #<start>-<end>[;<step>]
// server defaults to "irc.rizon.net" if empty.
func ParseXDCCMessage(msg, directory, server string) ([]*XDCCPack, error) {
	if server == "" {
		server = "irc.rizon.net"
	}
	if directory == "" {
		directory = "."
	}

	if !xdccMsgRegex.MatchString(msg) {
		return nil, fmt.Errorf("invalid XDCC message: %s", msg)
	}

	// Extract bot name: "/msg <bot> xdcc send ..."
	afterMsg := strings.TrimPrefix(msg, "/msg ")
	parts := strings.SplitN(afterMsg, " ", 2)
	bot := parts[0]

	// Apply server overrides based on bot prefix
	ircServer := resolveServer(bot, server)

	// Extract pack number(s) after "#"
	packPart := msg[strings.LastIndex(msg, "#")+1:]

	var packs []*XDCCPack

	if strings.Contains(packPart, ",") {
		// Comma-separated: #1,2,3
		for _, n := range strings.Split(packPart, ",") {
			num, err := strconv.Atoi(strings.TrimSpace(n))
			if err != nil {
				return nil, fmt.Errorf("invalid pack number: %s", n)
			}
			p := NewXDCCPack(ircServer, bot, num)
			p.SetDirectory(directory)
			packs = append(packs, p)
		}
	} else if strings.Contains(packPart, "-") {
		// Range: #1-10 or #1-10;2
		step := 1
		rangeStr := packPart
		if strings.Contains(rangeStr, ";") {
			rangeParts := strings.SplitN(rangeStr, ";", 2)
			rangeStr = rangeParts[0]
			step, _ = strconv.Atoi(rangeParts[1])
			if step < 1 {
				step = 1
			}
		}
		rangeParts := strings.SplitN(rangeStr, "-", 2)
		start, err1 := strconv.Atoi(rangeParts[0])
		end, err2 := strconv.Atoi(rangeParts[1])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("invalid pack range: %s", packPart)
		}
		for i := start; i <= end; i += step {
			p := NewXDCCPack(ircServer, bot, i)
			p.SetDirectory(directory)
			packs = append(packs, p)
		}
	} else {
		num, err := strconv.Atoi(packPart)
		if err != nil {
			return nil, fmt.Errorf("invalid pack number: %s", packPart)
		}
		p := NewXDCCPack(ircServer, bot, num)
		p.SetDirectory(directory)
		packs = append(packs, p)
	}

	return packs, nil
}

// resolveServer returns the appropriate IrcServer for the given bot name.
// Applies known bot-prefix → server overrides only when the default server
// is irc.rizon.net (i.e. no explicit --server was provided by the user).
func resolveServer(bot, defaultServer string) IrcServer {
	if defaultServer != "irc.rizon.net" && defaultServer != "" {
		// User explicitly set a server — honour it regardless of bot prefix
		return ParseIrcServer(defaultServer)
	}
	switch {
	case strings.HasPrefix(bot, "TLT"):
		return NewIrcServer("irc.williamgattone.it")
	case strings.HasPrefix(bot, "WeC"):
		return NewIrcServer("irc.explosionirc.net")
	default:
		return ParseIrcServer(defaultServer)
	}
}

// PreparePacks applies output path and server overrides to a list of packs.
// If location is set and there is only one pack, it overrides the filename;
// for multiple packs, it appends a zero-padded index.
func PreparePacks(packs []*XDCCPack, location string) {
	// Apply server overrides based on bot name
	for _, p := range packs {
		p.Server = resolveServer(p.Bot, p.Server.Address)
	}

	if location == "" {
		return
	}
	if len(packs) == 1 {
		packs[0].SetFilename(location, true)
	} else {
		for i, p := range packs {
			p.SetFilename(fmt.Sprintf("%s-%03d", location, i), true)
		}
	}
}

// ByteStringToByteCount converts a human-readable byte string (e.g. "1.5 MB") to bytes.
func ByteStringToByteCount(s string) int64 {
	s = strings.TrimSpace(s)
	units := map[string]int64{
		"B": 1, "KB": 1024, "MB": 1024 * 1024, "GB": 1024 * 1024 * 1024,
		"K": 1024, "M": 1024 * 1024, "G": 1024 * 1024 * 1024,
	}
	for suffix, mult := range units {
		if strings.HasSuffix(strings.ToUpper(s), suffix) {
			numStr := strings.TrimSpace(s[:len(s)-len(suffix)])
			val, err := strconv.ParseFloat(numStr, 64)
			if err == nil {
				return int64(val * float64(mult))
			}
		}
	}
	// Try plain number
	val, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err == nil {
		return val
	}
	return 0
}

// ParseThrottle converts a throttle string (e.g. "50M", "100K") to bytes per second.
// Returns -1 if throttle is disabled (empty or "0" or "-1").
func ParseThrottle(s string) (int64, error) {
	if s == "" || s == "-1" || s == "0" {
		return -1, nil
	}
	upper := strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	switch {
	case strings.HasSuffix(upper, "G"):
		mult = 1024 * 1024 * 1024
		upper = upper[:len(upper)-1]
	case strings.HasSuffix(upper, "M"):
		mult = 1024 * 1024
		upper = upper[:len(upper)-1]
	case strings.HasSuffix(upper, "K"):
		mult = 1024
		upper = upper[:len(upper)-1]
	}
	val, err := strconv.ParseFloat(upper, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid throttle value: %s", s)
	}
	return int64(val * float64(mult)), nil
}
