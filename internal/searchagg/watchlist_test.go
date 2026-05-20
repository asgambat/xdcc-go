package searchagg

import (
	"testing"

	"xdcc-go/internal/entities"
)

// ===========================================================================
// computeFingerprint
// ===========================================================================

func TestComputeFingerprint_Empty(t *testing.T) {
	fp := computeFingerprint(nil)
	if fp != "" {
		t.Errorf("expected empty fingerprint for nil packs, got %q", fp)
	}

	fp = computeFingerprint([]*entities.XDCCPack{})
	if fp != "" {
		t.Errorf("expected empty fingerprint for empty packs, got %q", fp)
	}
}

func TestComputeFingerprint_Deterministic(t *testing.T) {
	packs := []*entities.XDCCPack{
		mkPackWithBot("file.mkv", 1000, "Bot", 1),
		mkPackWithBot("other.mkv", 2000, "OtherBot", 2),
	}

	fp1 := computeFingerprint(packs)
	fp2 := computeFingerprint(packs)
	if fp1 != fp2 {
		t.Errorf("fingerprint should be deterministic: %q != %q", fp1, fp2)
	}
	if fp1 == "" {
		t.Errorf("expected non-empty fingerprint")
	}
}

func TestComputeFingerprint_DifferentPacks(t *testing.T) {
	packs1 := []*entities.XDCCPack{
		mkPackWithBot("file.mkv", 1000, "Bot", 1),
	}
	packs2 := []*entities.XDCCPack{
		mkPackWithBot("different.mkv", 2000, "Bot", 2),
	}

	fp1 := computeFingerprint(packs1)
	fp2 := computeFingerprint(packs2)
	if fp1 == fp2 {
		t.Errorf("different packs should produce different fingerprints")
	}
}

func TestComputeFingerprint_OrderIndependent(t *testing.T) {
	packs1 := []*entities.XDCCPack{
		mkPackWithBot("a.mkv", 100, "Bot1", 1),
		mkPackWithBot("b.mkv", 200, "Bot2", 2),
	}
	packs2 := []*entities.XDCCPack{
		mkPackWithBot("b.mkv", 200, "Bot2", 2),
		mkPackWithBot("a.mkv", 100, "Bot1", 1),
	}

	fp1 := computeFingerprint(packs1)
	fp2 := computeFingerprint(packs2)
	if fp1 != fp2 {
		t.Errorf("fingerprint should be order-independent: %q != %q", fp1, fp2)
	}
}

func TestComputeFingerprint_MultiplePacks(t *testing.T) {
	packs := []*entities.XDCCPack{
		mkPackWithBot("a.mkv", 100, "Bot1", 1),
		mkPackWithBot("b.mkv", 200, "Bot2", 2),
		mkPackWithBot("c.mkv", 300, "Bot3", 3),
	}

	fp := computeFingerprint(packs)
	if fp == "" {
		t.Errorf("expected non-empty fingerprint for multiple packs")
	}
	if len(fp) != 64 { // SHA-256 hex is 64 chars
		t.Errorf("expected 64-char SHA-256 hex fingerprint, got %d chars", len(fp))
	}
}

// ===========================================================================
// findNewPacks
// ===========================================================================

func TestFindNewPacks_NoChange(t *testing.T) {
	packs := []*entities.XDCCPack{
		mkPackWithBot("a.mkv", 100, "Bot", 1),
	}
	fp := computeFingerprint(packs)

	newPacks := findNewPacks(packs, fp)
	if newPacks != nil {
		t.Errorf("expected nil (no changes) when fingerprint matches, got %d packs", len(newPacks))
	}
}

func TestFindNewPacks_Change(t *testing.T) {
	packs := []*entities.XDCCPack{
		mkPackWithBot("a.mkv", 100, "Bot", 1),
	}

	newPacks := findNewPacks(packs, "different_fingerprint")
	if newPacks == nil {
		t.Errorf("expected non-nil when fingerprint differs")
	} else if len(newPacks) != 1 {
		t.Errorf("expected 1 pack when fingerprint differs, got %d", len(newPacks))
	}
}

func TestFindNewPacks_EmptyPrevFingerprint(t *testing.T) {
	packs := []*entities.XDCCPack{
		mkPackWithBot("a.mkv", 100, "Bot", 1),
	}

	newPacks := findNewPacks(packs, "")
	// Empty previous fingerprint means first run — should return all packs
	if newPacks == nil {
		t.Errorf("expected all packs when prev fingerprint is empty")
	} else if len(newPacks) != 1 {
		t.Errorf("expected 1 pack, got %d", len(newPacks))
	}
}

// ===========================================================================
// WatchlistRunResult
// ===========================================================================

func TestWatchlistRunResult_HasChanges(t *testing.T) {
	// Verify the HasChanges field behavior
	result := &WatchlistRunResult{
		HasChanges: true,
		NewPacks: []*entities.XDCCPack{
			mkPackWithBot("new.mkv", 100, "Bot", 1),
		},
	}

	if !result.HasChanges {
		t.Errorf("expected HasChanges=true")
	}
	if len(result.NewPacks) != 1 {
		t.Errorf("expected 1 new pack, got %d", len(result.NewPacks))
	}
}

func TestWatchlistRunResult_NoChanges(t *testing.T) {
	result := &WatchlistRunResult{
		HasChanges: false,
		NewPacks:   nil,
	}

	if result.HasChanges {
		t.Errorf("expected HasChanges=false")
	}
	if result.NewPacks != nil {
		t.Errorf("expected NewPacks=nil")
	}
}
