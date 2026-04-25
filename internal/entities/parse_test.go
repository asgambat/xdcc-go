package entities

import "testing"

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

func TestPreparePacks_DirectoryLocation(t *testing.T) {
	dir := t.TempDir()
	packs := []*XDCCPack{
		NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 1),
		NewXDCCPack(NewIrcServer("irc.rizon.net"), "Bot", 2),
	}
	// Set original filenames to verify they are not overwritten.
	packs[0].SetFilename("file1.mkv", true)
	packs[1].SetFilename("file2.mkv", true)

	PreparePacks(packs, dir)

	for i, p := range packs {
		if p.Directory != dir {
			t.Errorf("packs[%d].Directory = %q, want %q", i, p.Directory, dir)
		}
	}
	// Filenames must remain unchanged.
	if packs[0].Filename != "file1.mkv" {
		t.Errorf("packs[0].Filename = %q, want file1.mkv", packs[0].Filename)
	}
	if packs[1].Filename != "file2.mkv" {
		t.Errorf("packs[1].Filename = %q, want file2.mkv", packs[1].Filename)
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
