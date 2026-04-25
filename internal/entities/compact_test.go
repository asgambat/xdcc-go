package entities

import "testing"

func TestBotFamily(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		// len >= 13 → first 10
		{"Bot1234567890", "Bot1234567"},
		{"WONDERFULBOT!", "WONDERFULB"},
		{"1234567890ABC", "1234567890"},
		// len < 13 and > 3 → first len-3
		{"Bot123456789", "Bot123456"},  // len=12 → 9
		{"BotABCD", "BotA"},           // len=7 → 4
		{"ABCD", "A"},                 // len=4 → 1
		// len <= 3 → full name
		{"Bot", "Bot"},
		{"AB", "AB"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BotFamily(tt.name)
			if got != tt.want {
				t.Errorf("BotFamily(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestCompactPacks(t *testing.T) {
	srv := IrcServer{Address: "irc.rizon.net"}

	mkPack := func(filename string, size int64, bot string, packNum int) *XDCCPack {
		p := NewXDCCPack(srv, bot, packNum)
		p.Filename = filename
		p.Size = size
		return p
	}

	packs := []*XDCCPack{
		mkPack("file.mkv", 1000, "Bot1234567890", 1), // family "Bot1234567"
		mkPack("file.mkv", 1000, "Bot1234567XXX", 2), // same family → duplicate
		mkPack("file.mkv", 1000, "Bot1234567YYY", 3), // same family → duplicate
		mkPack("file.mkv", 2000, "Bot1234567890", 4), // different size → kept
		mkPack("other.mkv", 1000, "Bot1234567890", 5), // different filename → kept
		mkPack("file.mkv", 1000, "DiffBot123456", 6),  // different family → kept
	}

	result := CompactPacks(packs)

	if len(result) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result))
	}

	expectedPackNums := []int{1, 4, 5, 6}
	for i, p := range result {
		if p.PackNumber != expectedPackNums[i] {
			t.Errorf("result[%d].PackNumber = %d, want %d", i, p.PackNumber, expectedPackNums[i])
		}
	}
}

func TestCompactPacks_NoDuplicates(t *testing.T) {
	srv := IrcServer{Address: "irc.rizon.net"}

	mkPack := func(filename string, size int64, bot string, packNum int) *XDCCPack {
		p := NewXDCCPack(srv, bot, packNum)
		p.Filename = filename
		p.Size = size
		return p
	}

	packs := []*XDCCPack{
		mkPack("a.mkv", 100, "BotA1234567890", 1),
		mkPack("b.mkv", 200, "BotB1234567890", 2),
	}

	result := CompactPacks(packs)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
}

func TestCompactPacks_Empty(t *testing.T) {
	result := CompactPacks(nil)
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}
}
