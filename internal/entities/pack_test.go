package entities

import (
	"path/filepath"
	"strings"
	"testing"
)

// --- XDCCPack methods --------------------------------------------------------

func TestGetRequestMessage(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "CoolBot", 42)
	if got := p.GetRequestMessage(false); got != "xdcc send #42" {
		t.Errorf("short = %q", got)
	}
	if got := p.GetRequestMessage(true); got != "/msg CoolBot xdcc send #42" {
		t.Errorf("full = %q", got)
	}
}

func TestGetFilepath_DotDirectory(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetFilename("file.mkv", true)
	p.SetDirectory(".")
	if got := p.GetFilepath(); got != "file.mkv" {
		t.Errorf("GetFilepath (dot dir) = %q, want file.mkv", got)
	}
}

func TestGetFilepath_AbsoluteDirectory(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetFilename("file.mkv", true)
	p.SetDirectory("/downloads")
	want := filepath.Join("/downloads", "file.mkv")
	if got := p.GetFilepath(); got != want {
		t.Errorf("GetFilepath = %q, want %q", got, want)
	}
}

func TestGetFilepath_EmptyDirectory(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetFilename("file.mkv", true)
	p.SetDirectory("")
	if got := p.GetFilepath(); got != "file.mkv" {
		t.Errorf("GetFilepath (empty dir) = %q, want file.mkv", got)
	}
}

func TestSetFilename_Override(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetFilename("original.mkv", true)
	p.SetFilename("replacement.mkv", true)
	if p.Filename != "replacement.mkv" {
		t.Errorf("Filename = %q, want replacement.mkv", p.Filename)
	}
}

func TestSetFilename_NoOverrideAddsExtension(t *testing.T) {
	// When override=false and the current filename lacks the extension, it is appended.
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetFilename("myfile", true)
	p.SetFilename("something.mkv", false)
	if p.Filename != "myfile.mkv" {
		t.Errorf("Filename = %q, want myfile.mkv", p.Filename)
	}
}

func TestSetFilename_NoOverrideMultiDotFilename(t *testing.T) {
	// Multi-dot filename: extension should be ".mkv", not ".show.s01e01.mkv".
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetFilename("myfile", true)
	p.SetFilename("my.show.s01e01.mkv", false)
	if p.Filename != "myfile.mkv" {
		t.Errorf("Filename = %q, want myfile.mkv", p.Filename)
	}
}

func TestSetFilename_NoOverrideAlreadyHasExtension(t *testing.T) {
	// Extension already present → no change.
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetFilename("myfile.mkv", true)
	p.SetFilename("other.mkv", false)
	if p.Filename != "myfile.mkv" {
		t.Errorf("Filename = %q, want myfile.mkv", p.Filename)
	}
}

func TestSetFilename_NoOverrideEmptyFilename(t *testing.T) {
	// Filename not yet set → set it regardless of override flag.
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetFilename("new.mkv", false)
	if p.Filename != "new.mkv" {
		t.Errorf("Filename = %q, want new.mkv", p.Filename)
	}
}

func TestIsFilenameValid_NoOriginal(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	// No OriginalFilename set → always valid.
	if !p.IsFilenameValid("anything.mkv") {
		t.Error("expected true when OriginalFilename is empty")
	}
}

func TestIsFilenameValid_WithOriginal(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetOriginalFilename("expected.mkv")
	if !p.IsFilenameValid("expected.mkv") {
		t.Error("expected true for matching filename")
	}
	if p.IsFilenameValid("other.mkv") {
		t.Error("expected false for non-matching filename")
	}
}

func TestSetSize(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetSize(1024 * 1024)
	if p.Size != 1024*1024 {
		t.Errorf("Size = %d, want 1048576", p.Size)
	}
}

func TestString(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 5)
	p.SetFilename("ep01.mkv", true)
	p.SetSize(1024)
	got := p.String()
	if got == "" {
		t.Error("String() returned empty string")
	}
}

// --- HumanReadableBytes ------------------------------------------------------

func TestHumanReadableBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{int64(1.5 * 1024 * 1024), "1.5 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, tt := range tests {
		got := HumanReadableBytes(tt.in)
		if got != tt.want {
			t.Errorf("HumanReadableBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestString_ContainsAllParts(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "TestBot", 42)
	p.SetFilename("episode.mkv", true)
	p.SetSize(1024 * 1024)
	got := p.String()
	if !strings.Contains(got, "episode.mkv") {
		t.Errorf("String() = %q, want to contain filename", got)
	}
	if !strings.Contains(got, "TestBot") {
		t.Errorf("String() = %q, want to contain bot name", got)
	}
	if !strings.Contains(got, "#42") {
		t.Errorf("String() = %q, want to contain pack number", got)
	}
	if !strings.Contains(got, "MB") || !strings.Contains(got, "1.0") {
		t.Errorf("String() = %q, want to contain human-readable size", got)
	}
}

func TestHumanReadableBytes_TB(t *testing.T) {
	tb := int64(1024) * 1024 * 1024 * 1024
	got := HumanReadableBytes(tb)
	if got != "1.0 TB" {
		t.Errorf("HumanReadableBytes(1TB) = %q, want '1.0 TB'", got)
	}
}

func TestHumanReadableBytes_Negative(t *testing.T) {
	got := HumanReadableBytes(-1)
	// Negative values: the function uses int64, -1 < unit(1024) → falls into first branch
	if got != "-1 B" {
		t.Errorf("HumanReadableBytes(-1) = %q, want '-1 B'", got)
	}
}

func TestGetRequestMessage_LargePackNumber(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 999999)
	full := p.GetRequestMessage(true)
	if full != "/msg Bot xdcc send #999999" {
		t.Errorf("GetRequestMessage = %q", full)
	}
}

func TestSetOriginalFilename_Direct(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetOriginalFilename("expected.mkv")
	if p.OriginalFilename != "expected.mkv" {
		t.Errorf("OriginalFilename = %q, want expected.mkv", p.OriginalFilename)
	}
	if !p.IsFilenameValid("expected.mkv") {
		t.Error("IsFilenameValid should return true for matching filename")
	}
	if p.IsFilenameValid("other.mkv") {
		t.Error("IsFilenameValid should return false for non-matching filename")
	}
}
