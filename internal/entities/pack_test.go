package entities

import (
	"testing"
)

// --- ParseIrcServer ----------------------------------------------------------

func TestParseIrcServer_HostOnly(t *testing.T) {
	s := ParseIrcServer("irc.rizon.net")
	if s.Address != "irc.rizon.net" || s.Port != 6667 {
		t.Errorf("got %v", s)
	}
}

func TestParseIrcServer_HostPort(t *testing.T) {
	s := ParseIrcServer("irc.rizon.net:6697")
	if s.Address != "irc.rizon.net" || s.Port != 6697 {
		t.Errorf("got %v", s)
	}
}

func TestParseIrcServer_InvalidPort(t *testing.T) {
	// Non-numeric port → falls back to default 6667.
	s := ParseIrcServer("irc.rizon.net:abc")
	if s.Port != 6667 {
		t.Errorf("expected port 6667, got %d", s.Port)
	}
}

// --- resolveServer -----------------------------------------------------------

func TestResolveServer_ExplicitServer(t *testing.T) {
	// A non-default explicit server must always be honoured, regardless of bot prefix.
	s := resolveServer("TLTBot", "irc.custom.net")
	if s.Address != "irc.custom.net" {
		t.Errorf("expected irc.custom.net, got %s", s.Address)
	}
}

func TestResolveServer_TLTPrefix(t *testing.T) {
	s := resolveServer("TLTBot", "irc.rizon.net")
	if s.Address != "irc.williamgattone.it" {
		t.Errorf("expected irc.williamgattone.it, got %s", s.Address)
	}
}

func TestResolveServer_WeCPrefix(t *testing.T) {
	s := resolveServer("WeCBot", "irc.rizon.net")
	if s.Address != "irc.explosionirc.net" {
		t.Errorf("expected irc.explosionirc.net, got %s", s.Address)
	}
}

func TestResolveServer_Default(t *testing.T) {
	s := resolveServer("SomeBot", "irc.rizon.net")
	if s.Address != "irc.rizon.net" {
		t.Errorf("expected irc.rizon.net, got %s", s.Address)
	}
}

// --- ParseXDCCMessage --------------------------------------------------------

func TestParseXDCCMessage_Single(t *testing.T) {
	packs, err := ParseXDCCMessage("/msg SomeBot xdcc send #42", ".", "irc.rizon.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("got %d packs, want 1", len(packs))
	}
	if packs[0].PackNumber != 42 || packs[0].Bot != "SomeBot" {
		t.Errorf("pack = %+v", packs[0])
	}
}

func TestParseXDCCMessage_CommaSeparated(t *testing.T) {
	packs, err := ParseXDCCMessage("/msg SomeBot xdcc send #1,3,5", ".", "irc.rizon.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 3 {
		t.Fatalf("got %d packs, want 3", len(packs))
	}
	for i, want := range []int{1, 3, 5} {
		if packs[i].PackNumber != want {
			t.Errorf("packs[%d].PackNumber = %d, want %d", i, packs[i].PackNumber, want)
		}
	}
}

func TestParseXDCCMessage_Range(t *testing.T) {
	packs, err := ParseXDCCMessage("/msg SomeBot xdcc send #1-4", ".", "irc.rizon.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 4 {
		t.Fatalf("got %d packs, want 4", len(packs))
	}
	for i, want := range []int{1, 2, 3, 4} {
		if packs[i].PackNumber != want {
			t.Errorf("packs[%d].PackNumber = %d, want %d", i, packs[i].PackNumber, want)
		}
	}
}

func TestParseXDCCMessage_RangeWithStep(t *testing.T) {
	packs, err := ParseXDCCMessage("/msg SomeBot xdcc send #1-9;2", ".", "irc.rizon.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 5 {
		t.Fatalf("got %d packs, want 5", len(packs))
	}
	for i, want := range []int{1, 3, 5, 7, 9} {
		if packs[i].PackNumber != want {
			t.Errorf("packs[%d].PackNumber = %d, want %d", i, packs[i].PackNumber, want)
		}
	}
}

func TestParseXDCCMessage_DefaultsApplied(t *testing.T) {
	// Empty server → irc.rizon.net; empty directory → ".".
	packs, err := ParseXDCCMessage("/msg SomeBot xdcc send #10", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if packs[0].Server.Address != "irc.rizon.net" {
		t.Errorf("Server.Address = %s, want irc.rizon.net", packs[0].Server.Address)
	}
	if packs[0].Directory != "." {
		t.Errorf("Directory = %s, want .", packs[0].Directory)
	}
}

func TestParseXDCCMessage_BotServerOverride(t *testing.T) {
	// TLT-prefixed bot should resolve to irc.williamgattone.it.
	packs, err := ParseXDCCMessage("/msg TLTBot xdcc send #1", ".", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if packs[0].Server.Address != "irc.williamgattone.it" {
		t.Errorf("Server.Address = %s, want irc.williamgattone.it", packs[0].Server.Address)
	}
}

func TestParseXDCCMessage_InvalidFormat(t *testing.T) {
	_, err := ParseXDCCMessage("not a valid message", ".", "")
	if err == nil {
		t.Error("expected error for invalid message, got nil")
	}
}

func TestParseXDCCMessage_DirectoryPropagated(t *testing.T) {
	packs, err := ParseXDCCMessage("/msg SomeBot xdcc send #1,2", "/downloads", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range packs {
		if p.Directory != "/downloads" {
			t.Errorf("Directory = %s, want /downloads", p.Directory)
		}
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

// --- ByteStringToByteCount ---------------------------------------------------

func TestByteStringToByteCount(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"1 KB", 1024},
		{"1 MB", 1024 * 1024},
		{"1 GB", 1024 * 1024 * 1024},
		{"1.5 MB", int64(1.5 * 1024 * 1024)},
		{"512 B", 512},
		{"1024", 1024},
		{"", 0},
		{"abc", 0},
	}
	for _, tt := range tests {
		got := ByteStringToByteCount(tt.in)
		if got != tt.want {
			t.Errorf("ByteStringToByteCount(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// --- ParseThrottle -----------------------------------------------------------

func TestParseThrottle(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"", -1, false},
		{"0", -1, false},
		{"-1", -1, false},
		{"100K", 100 * 1024, false},
		{"2M", 2 * 1024 * 1024, false},
		{"1G", 1024 * 1024 * 1024, false},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseThrottle(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseThrottle(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if err == nil && got != tt.want {
			t.Errorf("ParseThrottle(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

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

func TestGetFilepath(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetFilename("file.mkv", true)

	p.SetDirectory(".")
	if got := p.GetFilepath(); got != "file.mkv" {
		t.Errorf("GetFilepath (dot dir) = %q, want file.mkv", got)
	}

	p.SetDirectory("/downloads")
	if got := p.GetFilepath(); got != "/downloads/file.mkv" {
		t.Errorf("GetFilepath = %q, want /downloads/file.mkv", got)
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

// --- PreparePacks ------------------------------------------------------------

func TestPreparePacks_SinglePackLocation(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	PreparePacks([]*XDCCPack{p}, "custom_name")
	if p.Filename != "custom_name" {
		t.Errorf("Filename = %q, want custom_name", p.Filename)
	}
}

func TestPreparePacks_MultiplePacksLocation(t *testing.T) {
	packs := []*XDCCPack{
		NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1),
		NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 2),
	}
	PreparePacks(packs, "ep")
	if packs[0].Filename != "ep-000" {
		t.Errorf("packs[0].Filename = %q, want ep-000", packs[0].Filename)
	}
	if packs[1].Filename != "ep-001" {
		t.Errorf("packs[1].Filename = %q, want ep-001", packs[1].Filename)
	}
}

func TestPreparePacks_NoLocation(t *testing.T) {
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1)
	p.SetFilename("original.mkv", true)
	PreparePacks([]*XDCCPack{p}, "")
	if p.Filename != "original.mkv" {
		t.Errorf("filename should not change when location is empty")
	}
}

func TestPreparePacks_TLTBotServerOverride(t *testing.T) {
	// PreparePacks applies resolveServer based on the bot name.
	p := NewXDCCPack(NewIrcServer("irc.rizon.net"), "TLTBot", 1)
	PreparePacks([]*XDCCPack{p}, "")
	if p.Server.Address != "irc.williamgattone.it" {
		t.Errorf("Server.Address = %s, want irc.williamgattone.it", p.Server.Address)
	}
}
