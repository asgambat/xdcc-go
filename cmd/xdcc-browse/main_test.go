package main

import (
	"testing"

	"xdcc-go/internal/entities"
)

// --- filterByBot ---

func TestFilterByBot_CaseInsensitive(t *testing.T) {
	packs := []*entities.XDCCPack{
		entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "WoNdBot01", 1),
		entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "OtherBot", 2),
		entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "WONDBOT02", 3),
	}
	result := filterByBot(packs, "wond")
	if len(result) != 2 {
		t.Fatalf("got %d packs, want 2", len(result))
	}
}

func TestFilterByBot_NoMatch(t *testing.T) {
	packs := []*entities.XDCCPack{
		entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot1", 1),
	}
	result := filterByBot(packs, "xyz")
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestFilterByBot_EmptyInput(t *testing.T) {
	result := filterByBot(nil, "test")
	if len(result) != 0 {
		t.Errorf("expected 0 results for nil input, got %d", len(result))
	}
}

// --- filterByExtension ---

func TestFilterByExtension_SingleExt(t *testing.T) {
	packs := []*entities.XDCCPack{
		mkPackWithName("file.mkv", 1),
		mkPackWithName("file.avi", 2),
		mkPackWithName("file.mkv", 3),
	}
	result := filterByExtension(packs, "mkv")
	if len(result) != 2 {
		t.Fatalf("got %d packs, want 2", len(result))
	}
}

func TestFilterByExtension_MultipleExts(t *testing.T) {
	packs := []*entities.XDCCPack{
		mkPackWithName("file.mkv", 1),
		mkPackWithName("file.avi", 2),
		mkPackWithName("file.mp4", 3),
		mkPackWithName("file.srt", 4),
	}
	result := filterByExtension(packs, "mkv,mp4")
	if len(result) != 2 {
		t.Fatalf("got %d packs, want 2", len(result))
	}
}

func TestFilterByExtension_WithDot(t *testing.T) {
	packs := []*entities.XDCCPack{
		mkPackWithName("file.mkv", 1),
	}
	result := filterByExtension(packs, ".mkv")
	if len(result) != 1 {
		t.Fatalf("got %d packs, want 1", len(result))
	}
}

func TestFilterByExtension_CaseInsensitive(t *testing.T) {
	packs := []*entities.XDCCPack{
		mkPackWithName("file.MKV", 1),
	}
	result := filterByExtension(packs, "mkv")
	if len(result) != 1 {
		t.Fatalf("got %d packs, want 1", len(result))
	}
}

func TestFilterByExtension_NoMatch(t *testing.T) {
	packs := []*entities.XDCCPack{
		mkPackWithName("file.mkv", 1),
	}
	result := filterByExtension(packs, "avi")
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

// --- parseSelection ---

func TestParseSelection_SingleNumber(t *testing.T) {
	packs := makePacks(5)
	result, err := parseSelection("3", packs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].PackNumber != 3 {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestParseSelection_Range(t *testing.T) {
	packs := makePacks(5)
	result, err := parseSelection("2-4", packs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("got %d packs, want 3", len(result))
	}
}

func TestParseSelection_Count(t *testing.T) {
	packs := makePacks(10)
	result, err := parseSelection("3+4", packs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 4 {
		t.Fatalf("got %d packs, want 4", len(result))
	}
	for i, want := range []int{3, 4, 5, 6} {
		if result[i].PackNumber != want {
			t.Errorf("result[%d].PackNumber = %d, want %d", i, result[i].PackNumber, want)
		}
	}
}

func TestParseSelection_CommaList(t *testing.T) {
	packs := makePacks(5)
	result, err := parseSelection("1,3,5", packs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("got %d packs, want 3", len(result))
	}
}

func TestParseSelection_Dedup(t *testing.T) {
	packs := makePacks(5)
	result, err := parseSelection("1,1,1", packs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Errorf("expected dedup to produce 1 result, got %d", len(result))
	}
}

func TestParseSelection_OutOfRange(t *testing.T) {
	packs := makePacks(3)
	_, err := parseSelection("5", packs)
	if err == nil {
		t.Error("expected error for out-of-range index")
	}
}

func TestParseSelection_InvalidRange(t *testing.T) {
	packs := makePacks(5)
	_, err := parseSelection("5-3", packs)
	if err == nil {
		t.Error("expected error for reversed range")
	}
}

func TestParseSelection_InvalidInput(t *testing.T) {
	packs := makePacks(5)
	_, err := parseSelection("abc", packs)
	if err == nil {
		t.Error("expected error for non-numeric input")
	}
}

func TestParseSelection_Zero(t *testing.T) {
	packs := makePacks(5)
	_, err := parseSelection("0", packs)
	if err == nil {
		t.Error("expected error for zero index")
	}
}

func TestParseSelection_NegativeNumber(t *testing.T) {
	// "-1" would be parsed as a range "" to "1", which should fail
	packs := makePacks(5)
	_, err := parseSelection("-1", packs)
	if err == nil {
		t.Error("expected error for negative-like input")
	}
}

// --- helpers ---

func mkPackWithName(name string, packNum int) *entities.XDCCPack {
	p := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", packNum)
	p.SetFilename(name, true)
	return p
}

func makePacks(n int) []*entities.XDCCPack {
	packs := make([]*entities.XDCCPack, n)
	for i := 0; i < n; i++ {
		packs[i] = entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", i+1)
	}
	return packs
}
