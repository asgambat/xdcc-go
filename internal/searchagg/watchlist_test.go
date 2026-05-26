package searchagg

import (
	"context"
	"testing"

	"xdcc-go/internal/entities"
	"xdcc-go/internal/store"
)

// ===========================================================================
// Mock store for testing filterNewPacks
// ===========================================================================

type mockDownloadStore struct {
	existingFilenames map[string]bool
}

func (m *mockDownloadStore) FilenamesExist(ctx context.Context, filenames []string) (map[string]bool, error) {
	result := make(map[string]bool, len(filenames))
	for _, fn := range filenames {
		if m.existingFilenames[fn] {
			result[fn] = true
		} else {
			result[fn] = false
		}
	}
	return result, nil
}

// Stub methods to satisfy the DownloadStore interface
func (m *mockDownloadStore) EnqueueDownload(ctx context.Context, d store.DownloadRecord) (int64, error) {
	return 1, nil
}
func (m *mockDownloadStore) GetDownload(ctx context.Context, id int64) (*store.DownloadRecord, error) {
	return nil, nil
}
func (m *mockDownloadStore) GetQueue(ctx context.Context) ([]store.DownloadRecord, error) {
	return nil, nil
}
func (m *mockDownloadStore) GetQueueByChannel(ctx context.Context, channel string) ([]store.DownloadRecord, error) {
	return nil, nil
}
func (m *mockDownloadStore) GetActiveDownloads(ctx context.Context) ([]store.DownloadRecord, error) {
	return nil, nil
}
func (m *mockDownloadStore) GetPendingByChannel(ctx context.Context, channel string) ([]store.DownloadRecord, error) {
	return nil, nil
}
func (m *mockDownloadStore) UpdateDownloadProgress(ctx context.Context, id int64, progressBytes int64, speedBPS int64) error {
	return nil
}
func (m *mockDownloadStore) MarkDownloadStarted(ctx context.Context, id int64) error { return nil }
func (m *mockDownloadStore) MarkDownloadCompleted(ctx context.Context, id int64, filename string, fileSize int64) error {
	return nil
}
func (m *mockDownloadStore) MarkDownloadFailed(ctx context.Context, id int64, errMsg string) error {
	return nil
}
func (m *mockDownloadStore) MarkDownloadSkipped(ctx context.Context, id int64) error { return nil }
func (m *mockDownloadStore) MarkDownloadPaused(ctx context.Context, id int64) error  { return nil }
func (m *mockDownloadStore) MarkDownloadRetry(ctx context.Context, id int64, newStatus string) error {
	return nil
}
func (m *mockDownloadStore) DeleteDownload(ctx context.Context, id int64) error { return nil }
func (m *mockDownloadStore) RetryDownload(ctx context.Context, id int64) error  { return nil }
func (m *mockDownloadStore) GetDownloadHistory(ctx context.Context, _, _ int, _ store.HistoryFilter) ([]store.DownloadRecord, int, error) {
	return nil, 0, nil
}
func (m *mockDownloadStore) GetTotalDownloadedBytes(ctx context.Context) (int64, error) {
	return 0, nil
}
func (m *mockDownloadStore) RecoverDownloadsOnStartup(ctx context.Context) ([]store.DownloadRecord, error) {
	return nil, nil
}
func (m *mockDownloadStore) RequeueDownload(ctx context.Context, id int64) error { return nil }
func (m *mockDownloadStore) SetDownloadPriority(ctx context.Context, id int64, priority int) error {
	return nil
}
func (m *mockDownloadStore) UpdateDownloadMetadata(ctx context.Context, id int64, filename string, fileSize int64) error {
	return nil
}
func (m *mockDownloadStore) BulkActionDownloads(ctx context.Context, ids []int64, action string) (map[int64]string, error) {
	return nil, nil
}
func (m *mockDownloadStore) FindDuplicateDownload(ctx context.Context, bot, serverAddress string, packNumber int) (*store.DownloadRecord, error) {
	return nil, nil
}
func (m *mockDownloadStore) GetDownloadByBotMessage(ctx context.Context, bot, packMessage string) (*store.DownloadRecord, error) {
	return nil, nil
}

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
// filterNewPacks
// ===========================================================================

func TestFilterNewPacks_EmptyPacks(t *testing.T) {
	ms := &mockDownloadStore{}
	packs := filterNewPacks(context.Background(), ms, nil)
	if packs != nil {
		t.Errorf("expected nil for nil packs, got %d packs", len(packs))
	}

	packs = filterNewPacks(context.Background(), ms, []*entities.XDCCPack{})
	if packs != nil {
		t.Errorf("expected nil for empty packs, got %d packs", len(packs))
	}
}

func TestFilterNewPacks_AllNew(t *testing.T) {
	ms := &mockDownloadStore{existingFilenames: map[string]bool{}}
	packs := []*entities.XDCCPack{
		mkPackWithBot("a.mkv", 100, "Bot", 1),
		mkPackWithBot("b.mkv", 200, "Bot", 2),
	}

	newPacks := filterNewPacks(context.Background(), ms, packs)
	if len(newPacks) != 2 {
		t.Errorf("expected 2 new packs, got %d", len(newPacks))
	}
}

func TestFilterNewPacks_SomeAlreadyDownloaded(t *testing.T) {
	ms := &mockDownloadStore{existingFilenames: map[string]bool{"a.mkv": true}}
	packs := []*entities.XDCCPack{
		mkPackWithBot("a.mkv", 100, "Bot", 1),
		mkPackWithBot("b.mkv", 200, "Bot", 2),
	}

	newPacks := filterNewPacks(context.Background(), ms, packs)
	if len(newPacks) != 1 {
		t.Errorf("expected 1 new pack (b.mkv), got %d", len(newPacks))
	}
	if newPacks[0].Filename != "b.mkv" {
		t.Errorf("expected b.mkv as new pack, got %s", newPacks[0].Filename)
	}
}

func TestFilterNewPacks_AllAlreadyDownloaded(t *testing.T) {
	ms := &mockDownloadStore{existingFilenames: map[string]bool{"a.mkv": true, "b.mkv": true}}
	packs := []*entities.XDCCPack{
		mkPackWithBot("a.mkv", 100, "Bot", 1),
		mkPackWithBot("b.mkv", 200, "Bot", 2),
	}

	newPacks := filterNewPacks(context.Background(), ms, packs)
	if len(newPacks) != 0 {
		t.Errorf("expected 0 new packs, got %d", len(newPacks))
	}
}

func TestFilterNewPacks_CaseInsensitive(t *testing.T) {
	ms := &mockDownloadStore{existingFilenames: map[string]bool{"a.mkv": true}}
	packs := []*entities.XDCCPack{
		mkPackWithBot("A.MKV", 100, "Bot", 1), // different case
	}

	// The mock store uses exact match (not LOWER), but the real SQLite uses LOWER()
	// The filterNewPacks function lowercases internally, so it will match
	newPacks := filterNewPacks(context.Background(), ms, packs)
	if len(newPacks) != 0 {
		t.Errorf("expected 0 new packs (case-insensitive match), got %d", len(newPacks))
	}
}

// ===========================================================================
// WatchlistRunResult
// ===========================================================================

func TestWatchlistRunResult_HasChanges(t *testing.T) {
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
