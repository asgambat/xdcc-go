package entities

import (
	"fmt"
	"path/filepath"
	"strings"
)

// XDCCPack models an XDCC pack to be downloaded from an IRC bot.
type XDCCPack struct {
	Server           IrcServer `json:"server"`
	Bot              string    `json:"bot"`
	Channel          string    `json:"channel,omitempty"`
	PackNumber       int       `json:"pack_number"`
	Directory        string    `json:"directory,omitempty"`
	Filename         string    `json:"filename"`
	OriginalFilename string    `json:"original_filename,omitempty"`
	Size             int64     `json:"size"`
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
		ext := filepath.Ext(filename)
		if ext != "" && !strings.HasSuffix(p.Filename, ext) {
			p.Filename += ext
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
	return filepath.Join(p.Directory, p.Filename)
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
