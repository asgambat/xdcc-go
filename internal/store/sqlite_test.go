package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore creates a SQLiteStore backed by a temporary in-memory file.
func newTestStore(tb testing.TB) *SQLiteStore {
	tb.Helper()
	dir := tb.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		tb.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := s.Migrate(); err != nil {
		tb.Fatalf("Migrate: %v", err)
	}
	return s
}

func closeStore(tb testing.TB, s *SQLiteStore) {
	tb.Helper()
	if err := s.Close(); err != nil {
		tb.Errorf("Close: %v", err)
	}
}

// ===========================================================================
// Server CRUD
// ===========================================================================

func TestAddServer(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, err := s.AddServer(ServerRecord{
		Address:     "irc.test.net",
		Port:        6667,
		AutoConnect: true,
		Status:      "disconnected",
	})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
}

func TestGetServer_NotFound(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	srv, err := s.GetServer(999)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv != nil {
		t.Errorf("expected nil for missing server, got %+v", srv)
	}
}

func TestGetServer_Found(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddServer(ServerRecord{Address: "irc.example.com", Port: 6667, Status: "disconnected"})
	srv, err := s.GetServer(id)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.Address != "irc.example.com" {
		t.Errorf("expected address irc.example.com, got %s", srv.Address)
	}
	if srv.Port != 6667 {
		t.Errorf("expected port 6667, got %d", srv.Port)
	}
	if srv.ID != id {
		t.Errorf("expected id %d, got %d", id, srv.ID)
	}
}

func TestListServers_Empty(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	servers, err := s.ListServers()
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(servers) != 0 {
		t.Errorf("expected empty list, got %d", len(servers))
	}
}

func TestListServers_Multiple(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	_, _ = s.AddServer(ServerRecord{Address: "irc.alpha.net", Port: 6667, Status: "disconnected"})
	_, _ = s.AddServer(ServerRecord{Address: "irc.beta.net", Port: 6667, Status: "connected"})

	servers, err := s.ListServers()
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(servers))
	}
}

func TestUpdateServer(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddServer(ServerRecord{Address: "irc.old.net", Port: 6667, Status: "disconnected"})
	err := s.UpdateServer(ServerRecord{ID: id, Address: "irc.new.net", Port: 6667, Status: "connected"})
	if err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}

	srv, _ := s.GetServer(id)
	if srv.Address != "irc.new.net" {
		t.Errorf("expected address updated to irc.new.net, got %s", srv.Address)
	}
}

func TestDeleteServer(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddServer(ServerRecord{Address: "irc.del.net", Port: 6667, Status: "disconnected"})
	err := s.DeleteServer(id)
	if err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}

	srv, _ := s.GetServer(id)
	if srv != nil {
		t.Errorf("expected deleted server to be nil, got %+v", srv)
	}
}

func TestSetServerStatus(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddServer(ServerRecord{Address: "irc.status.net", Port: 6667, Status: "disconnected"})
	err := s.SetServerStatus(id, "connected")
	if err != nil {
		t.Fatalf("SetServerStatus: %v", err)
	}

	srv, _ := s.GetServer(id)
	if srv.Status != "connected" {
		t.Errorf("expected status 'connected', got %s", srv.Status)
	}
}

func TestSetServerConnected(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddServer(ServerRecord{Address: "irc.conn.net", Port: 6667, Status: "disconnected", RetryCount: 3})
	err := s.SetServerConnected(id)
	if err != nil {
		t.Fatalf("SetServerConnected: %v", err)
	}

	srv, _ := s.GetServer(id)
	if srv.Status != "connected" {
		t.Errorf("expected status 'connected', got %s", srv.Status)
	}
	if srv.RetryCount != 0 {
		t.Errorf("expected retry_count reset to 0, got %d", srv.RetryCount)
	}
	if srv.LastConnectedAt == nil {
		t.Errorf("expected last_connected_at to be set")
	}
}

func TestIncrementServerRetry(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddServer(ServerRecord{Address: "irc.retry.net", Port: 6667, Status: "connected"})
	err := s.IncrementServerRetry(id)
	if err != nil {
		t.Fatalf("IncrementServerRetry: %v", err)
	}

	srv, _ := s.GetServer(id)
	if srv.RetryCount != 1 {
		t.Errorf("expected retry_count 1, got %d", srv.RetryCount)
	}
	if srv.Status != "reconnecting" {
		t.Errorf("expected status 'reconnecting', got %s", srv.Status)
	}
}

// ===========================================================================
// Channel CRUD
// ===========================================================================

func TestAddChannel(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	srvID, _ := s.AddServer(ServerRecord{Address: "irc.chan.net", Port: 6667})
	chID, err := s.AddChannel(ChannelRecord{
		ServerID: srvID,
		Name:     "#test",
		AutoJoin: true,
	})
	if err != nil {
		t.Fatalf("AddChannel: %v", err)
	}
	if chID <= 0 {
		t.Errorf("expected positive channel id, got %d", chID)
	}
}

func TestGetChannelsByServer(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	srvID, _ := s.AddServer(ServerRecord{Address: "irc.chan2.net", Port: 6667})
	_, _ = s.AddChannel(ChannelRecord{ServerID: srvID, Name: "#alpha"})
	_, _ = s.AddChannel(ChannelRecord{ServerID: srvID, Name: "#beta"})

	channels, err := s.GetChannelsByServer(srvID)
	if err != nil {
		t.Fatalf("GetChannelsByServer: %v", err)
	}
	if len(channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(channels))
	}

	// Wrong server ID
	empty, _ := s.GetChannelsByServer(999)
	if len(empty) != 0 {
		t.Errorf("expected 0 channels for non-existent server, got %d", len(empty))
	}
}

func TestGetChannelsByServerAndName(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	srvID, _ := s.AddServer(ServerRecord{Address: "irc.channelookup.net", Port: 6667})
	chID, _ := s.AddChannel(ChannelRecord{ServerID: srvID, Name: "#lookup"})

	ch, err := s.GetChannelsByServerAndName(srvID, "#lookup")
	if err != nil {
		t.Fatalf("GetChannelsByServerAndName: %v", err)
	}
	if ch == nil {
		t.Fatal("expected channel, got nil")
	}
	if ch.ID != chID {
		t.Errorf("expected id %d, got %d", chID, ch.ID)
	}

	// Not found
	missing, _ := s.GetChannelsByServerAndName(srvID, "#missing")
	if missing != nil {
		t.Errorf("expected nil for missing channel, got %+v", missing)
	}
}

func TestUpdateChannel(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	srvID, _ := s.AddServer(ServerRecord{Address: "irc.updchan.net", Port: 6667})
	chID, _ := s.AddChannel(ChannelRecord{ServerID: srvID, Name: "#old", AutoJoin: true})

	err := s.UpdateChannel(ChannelRecord{ID: chID, ServerID: srvID, Name: "#old", Topic: "new topic", AutoJoin: true, Joined: true})
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	ch, _ := s.GetChannelsByServerAndName(srvID, "#old")
	if ch.Topic != "new topic" {
		t.Errorf("expected topic 'new topic', got %s", ch.Topic)
	}
	if !ch.Joined {
		t.Errorf("expected joined=true")
	}
}

func TestSetChannelJoined(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	srvID, _ := s.AddServer(ServerRecord{Address: "irc.joined.net", Port: 6667})
	chID, _ := s.AddChannel(ChannelRecord{ServerID: srvID, Name: "#joinedtest"})

	_ = s.SetChannelJoined(chID, true)
	ch, _ := s.GetChannelsByServerAndName(srvID, "#joinedtest")
	if !ch.Joined {
		t.Errorf("expected joined=true after SetChannelJoined")
	}
}

func TestDeleteChannel(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	srvID, _ := s.AddServer(ServerRecord{Address: "irc.delchan.net", Port: 6667})
	chID, _ := s.AddChannel(ChannelRecord{ServerID: srvID, Name: "#delme"})

	_ = s.DeleteChannel(chID)
	channels, _ := s.GetChannelsByServer(srvID)
	if len(channels) != 0 {
		t.Errorf("expected 0 channels after delete, got %d", len(channels))
	}
}

func TestGetAutoJoinChannels(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	// Add a server with auto_connect=true
	srvID, _ := s.AddServer(ServerRecord{
		Address: "irc.auto.net", Port: 6667, AutoConnect: true, Status: "disconnected",
	})
	_, _ = s.AddChannel(ChannelRecord{ServerID: srvID, Name: "#auto1", AutoJoin: true})
	_, _ = s.AddChannel(ChannelRecord{ServerID: srvID, Name: "#auto2", AutoJoin: false})

	// Add another server with auto_connect=false — its channels should NOT be returned
	srvID2, _ := s.AddServer(ServerRecord{
		Address: "irc.manual.net", Port: 6667, AutoConnect: false,
	})
	_, _ = s.AddChannel(ChannelRecord{ServerID: srvID2, Name: "#manual", AutoJoin: true})

	autoChs, err := s.GetAutoJoinChannels()
	if err != nil {
		t.Fatalf("GetAutoJoinChannels: %v", err)
	}
	if len(autoChs) != 1 {
		t.Errorf("expected 1 auto-join channel, got %d", len(autoChs))
	}
	if len(autoChs) > 0 && autoChs[0].Name != "#auto1" {
		t.Errorf("expected #auto1, got %s", autoChs[0].Name)
	}
}

// ===========================================================================
// Download CRUD
// ===========================================================================

func TestEnqueueAndGetDownload(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, err := s.EnqueueDownload(DownloadRecord{
		PackMessage:   "xdcc send #1",
		Bot:           "TestBot",
		ServerAddress: "irc.test.net",
		Channel:       "#test",
		Filename:      "testfile.mkv",
		FileSize:      1000000,
	})
	if err != nil {
		t.Fatalf("EnqueueDownload: %v", err)
	}

	d, err := s.GetDownload(id)
	if err != nil {
		t.Fatalf("GetDownload: %v", err)
	}
	if d.Status != DownloadStatusQueued {
		t.Errorf("expected status 'queued', got %s", d.Status)
	}
	if d.Bot != "TestBot" {
		t.Errorf("expected bot TestBot, got %s", d.Bot)
	}
	if d.Filename != "testfile.mkv" {
		t.Errorf("expected filename testfile.mkv, got %s", d.Filename)
	}
}

func TestGetQueue(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	// Enqueue 3 downloads
	_, _ = s.EnqueueDownload(DownloadRecord{Bot: "Bot1", ServerAddress: "irc.t.net", Channel: "#a", Filename: "a.mkv", FileSize: 100})
	_, _ = s.EnqueueDownload(DownloadRecord{Bot: "Bot2", ServerAddress: "irc.t.net", Channel: "#b", Filename: "b.mkv", FileSize: 200})
	_, _ = s.EnqueueDownload(DownloadRecord{Bot: "Bot3", ServerAddress: "irc.t.net", Channel: "#c", Filename: "c.mkv", FileSize: 300})

	queue, err := s.GetQueue()
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	if len(queue) != 3 {
		t.Errorf("expected 3 items in queue, got %d", len(queue))
	}
}

func TestGetQueue_OrderedByPriority(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	// Enqueue with different priorities
	id1, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "BotA", ServerAddress: "irc.t.net", Channel: "#a", Filename: "low.mkv", FileSize: 100, Priority: 200,
	})
	id2, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "BotB", ServerAddress: "irc.t.net", Channel: "#a", Filename: "high.mkv", FileSize: 100, Priority: 50,
	})

	queue, _ := s.GetQueue()
	if len(queue) < 2 {
		t.Fatal("expected at least 2 queue items")
	}
	// Higher priority = lower number should come first
	if queue[0].ID != id2 {
		t.Errorf("expected higher priority item (id=%d) first, got id=%d", id2, queue[0].ID)
	}
	if queue[1].ID != id1 {
		t.Errorf("expected lower priority item (id=%d) second, got id=%d", id1, queue[1].ID)
	}
}

func TestGetQueueByChannel(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	_, _ = s.EnqueueDownload(DownloadRecord{Bot: "Bot1", ServerAddress: "irc.t.net", Channel: "#xdcc", Filename: "a.mkv", FileSize: 100})
	_, _ = s.EnqueueDownload(DownloadRecord{Bot: "Bot2", ServerAddress: "irc.t.net", Channel: "#other", Filename: "b.mkv", FileSize: 100})
	_, _ = s.EnqueueDownload(DownloadRecord{Bot: "Bot3", ServerAddress: "irc.t.net", Channel: "#xdcc", Filename: "c.mkv", FileSize: 100})

	queue, _ := s.GetQueueByChannel("#xdcc")
	if len(queue) != 2 {
		t.Errorf("expected 2 items for #xdcc, got %d", len(queue))
	}

	other, _ := s.GetQueueByChannel("#nonexistent")
	if len(other) != 0 {
		t.Errorf("expected 0 items for nonexistent channel, got %d", len(other))
	}
}

func TestMarkDownloadStarted(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 100,
	})
	err := s.MarkDownloadStarted(id)
	if err != nil {
		t.Fatalf("MarkDownloadStarted: %v", err)
	}

	d, _ := s.GetDownload(id)
	if d.Status != DownloadStatusDownloading {
		t.Errorf("expected status 'downloading', got %s", d.Status)
	}
	if d.StartedAt == nil {
		t.Errorf("expected started_at to be set")
	}
}

func TestUpdateDownloadProgress(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
	})

	_ = s.UpdateDownloadProgress(id, 500, 100)
	d, _ := s.GetDownload(id)
	if d.ProgressBytes != 500 {
		t.Errorf("expected progress 500, got %d", d.ProgressBytes)
	}
	if d.SpeedBPS != 100 {
		t.Errorf("expected speed 100, got %d", d.SpeedBPS)
	}
}

func TestMarkDownloadCompleted(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
	})
	_ = s.MarkDownloadStarted(id)
	_ = s.UpdateDownloadProgress(id, 1000, 0)

	err := s.MarkDownloadCompleted(id, "", 0)
	if err != nil {
		t.Fatalf("MarkDownloadCompleted: %v", err)
	}

	d, _ := s.GetDownload(id)
	if d.Status != DownloadStatusCompleted {
		t.Errorf("expected status 'completed', got %s", d.Status)
	}
	if d.CompletedAt == nil {
		t.Errorf("expected completed_at to be set")
	}
}

func TestMarkDownloadFailed(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
	})
	err := s.MarkDownloadFailed(id, "connection timeout")
	if err != nil {
		t.Fatalf("MarkDownloadFailed: %v", err)
	}

	d, _ := s.GetDownload(id)
	if d.Status != DownloadStatusFailed {
		t.Errorf("expected status 'failed', got %s", d.Status)
	}
	if d.ErrorMessage != "connection timeout" {
		t.Errorf("expected error message 'connection timeout', got %s", d.ErrorMessage)
	}
}

func TestMarkDownloadSkipped(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
	})
	_ = s.MarkDownloadStarted(id)

	err := s.MarkDownloadSkipped(id)
	if err != nil {
		t.Fatalf("MarkDownloadSkipped: %v", err)
	}

	d, _ := s.GetDownload(id)
	if d.Status != DownloadStatusSkipped {
		t.Errorf("expected status 'skipped_existing', got %s", d.Status)
	}
}

func TestMarkDownloadPaused(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
	})
	_ = s.MarkDownloadStarted(id)

	err := s.MarkDownloadPaused(id)
	if err != nil {
		t.Fatalf("MarkDownloadPaused: %v", err)
	}

	d, _ := s.GetDownload(id)
	if d.Status != DownloadStatusPaused {
		t.Errorf("expected status 'paused', got %s", d.Status)
	}
}

func TestMarkDownloadPaused_OnlyQueuedOrDownloading(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
	})
	_ = s.MarkDownloadCompleted(id, "", 0)

	// Pausing a completed download should be a no-op (no rows affected, but no error)
	err := s.MarkDownloadPaused(id)
	if err != nil {
		t.Fatalf("MarkDownloadPaused on completed: %v", err)
	}

	d, _ := s.GetDownload(id)
	if d.Status != DownloadStatusCompleted {
		t.Errorf("expected status still 'completed', got %s", d.Status)
	}
}

func TestRetryDownload(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
	})
	_ = s.MarkDownloadFailed(id, "some error")

	err := s.RetryDownload(id)
	if err != nil {
		t.Fatalf("RetryDownload: %v", err)
	}

	d, _ := s.GetDownload(id)
	if d.Status != DownloadStatusQueued {
		t.Errorf("expected status 'queued' after retry, got %s", d.Status)
	}
	if d.ProgressBytes != 0 {
		t.Errorf("expected progress_bytes reset to 0, got %d", d.ProgressBytes)
	}
	if d.ErrorMessage != "" {
		t.Errorf("expected error_message cleared, got %s", d.ErrorMessage)
	}
}

func TestDeleteDownload(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
	})
	_ = s.DeleteDownload(id)

	d, _ := s.GetDownload(id)
	if d != nil {
		t.Errorf("expected deleted download to be nil, got %+v", d)
	}
}

func TestSetDownloadPriority(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
	})

	err := s.SetDownloadPriority(id, 1)
	if err != nil {
		t.Fatalf("SetDownloadPriority: %v", err)
	}

	d, _ := s.GetDownload(id)
	if d.Priority != 1 {
		t.Errorf("expected priority 1, got %d", d.Priority)
	}
}

func TestFindDuplicateDownload(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	_, _ = s.EnqueueDownload(DownloadRecord{
		Bot: "MyBot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
		PackMessage: "xdcc send #5",
	})

	dup, err := s.FindDuplicateDownload("MyBot", "irc.t.net", 5)
	if err != nil {
		t.Fatalf("FindDuplicateDownload: %v", err)
	}
	if dup == nil {
		t.Fatal("expected duplicate to be found")
	}

	// Different pack number
	noDup, _ := s.FindDuplicateDownload("MyBot", "irc.t.net", 99)
	if noDup != nil {
		t.Errorf("expected no duplicate for different pack number, got %+v", noDup)
	}
}

func TestGetDownloadHistory(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	// Add completed downloads
	id1, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "a.mkv", FileSize: 100,
	})
	_ = s.MarkDownloadCompleted(id1, "", 0)

	id2, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "b.mkv", FileSize: 100,
	})
	_ = s.MarkDownloadFailed(id2, "error")

	// Queued download should NOT appear in history
	_, _ = s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "c.mkv", FileSize: 100,
	})

	history, total, err := s.GetDownloadHistory(1, 10, HistoryFilter{})
	if err != nil {
		t.Fatalf("GetDownloadHistory: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total 2 history items, got %d", total)
	}
	if len(history) != 2 {
		t.Errorf("expected 2 history items, got %d", len(history))
	}
}

func TestBulkActionDownloads(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id1, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "a.mkv", FileSize: 100,
	})
	id2, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "b.mkv", FileSize: 100,
	})

	results, err := s.BulkActionDownloads([]int64{id1, id2}, "pause")
	if err != nil {
		t.Fatalf("BulkActionDownloads: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	for id, r := range results {
		if r != "success" {
			t.Errorf("expected success for id %d, got %s", id, r)
		}
	}

	// Verify both are paused
	queue, _ := s.GetQueue()
	for _, d := range queue {
		if d.ID == id1 || d.ID == id2 {
			if d.Status != DownloadStatusPaused {
				t.Errorf("expected download %d to be paused, got %s", d.ID, d.Status)
			}
		}
	}
}

func TestBulkActionUnknownAction(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 100,
	})
	results, _ := s.BulkActionDownloads([]int64{id}, "unknown")
	if results[id] != "unknown action: unknown" {
		t.Errorf("expected 'unknown action: unknown', got %s", results[id])
	}
}

// ===========================================================================
// Search Cache
// ===========================================================================

func TestSetAndGetSearchCache(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	now := time.Now()
	entry := SearchCacheEntry{
		QueryKey:       "test query",
		Provider:       "nibl",
		PayloadJSON:    `[{"filename":"test.mkv","size":1000}]`,
		FetchedAt:      now,
		ExpiresAt:      now.Add(time.Hour),
		StaleExpiresAt: now.Add(24 * time.Hour),
	}

	err := s.SetSearchCache(entry)
	if err != nil {
		t.Fatalf("SetSearchCache: %v", err)
	}

	got, err := s.GetSearchCache("test query", "nibl")
	if err != nil {
		t.Fatalf("GetSearchCache: %v", err)
	}
	if got == nil {
		t.Fatal("expected cache entry, got nil")
	}
	if got.PayloadJSON != entry.PayloadJSON {
		t.Errorf("expected payload %s, got %s", entry.PayloadJSON, got.PayloadJSON)
	}
}

func TestGetSearchCache_Missing(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	entry, err := s.GetSearchCache("nonexistent", "nibl")
	if err != nil {
		t.Fatalf("GetSearchCache: %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil for missing cache entry, got %+v", entry)
	}
}

func TestDeleteExpiredSearchCache(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	now := time.Now()
	entry := SearchCacheEntry{
		QueryKey:       "stale query",
		Provider:       "xdcc_eu",
		PayloadJSON:    "[]",
		FetchedAt:      now.Add(-48 * time.Hour),
		ExpiresAt:      now.Add(-24 * time.Hour),
		StaleExpiresAt: now.Add(-1 * time.Hour),
	}
	_ = s.SetSearchCache(entry)

	err := s.DeleteExpiredSearchCache(now)
	if err != nil {
		t.Fatalf("DeleteExpiredSearchCache: %v", err)
	}

	got, _ := s.GetSearchCache("stale query", "xdcc_eu")
	if got != nil {
		t.Errorf("expected stale entry to be deleted, but got %+v", got)
	}
}

func TestGetSearchCacheByQuery(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	now := time.Now()
	// Insert entries from multiple providers for same query
	for _, prov := range []string{"nibl", "xdcc_eu", "sunxdcc"} {
		entry := SearchCacheEntry{
			QueryKey:       "multi provider",
			Provider:       prov,
			PayloadJSON:    `[{"filename":"` + prov + `.mkv"}]`,
			FetchedAt:      now,
			ExpiresAt:      now.Add(time.Hour),
			StaleExpiresAt: now.Add(24 * time.Hour),
		}
		if err := s.SetSearchCache(entry); err != nil {
			t.Fatalf("SetSearchCache(%s): %v", prov, err)
		}
	}

	// Insert entry for different query (should not appear)
	other := SearchCacheEntry{
		QueryKey:       "other query",
		Provider:       "nibl",
		PayloadJSON:    `[]`,
		FetchedAt:      now,
		ExpiresAt:      now.Add(time.Hour),
		StaleExpiresAt: now.Add(24 * time.Hour),
	}
	_ = s.SetSearchCache(other)

	entries, err := s.GetSearchCacheByQuery("multi provider")
	if err != nil {
		t.Fatalf("GetSearchCacheByQuery: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	providers := make(map[string]bool)
	for _, e := range entries {
		providers[e.Provider] = true
		if e.QueryKey != "multi provider" {
			t.Errorf("unexpected query_key: %s", e.QueryKey)
		}
	}
	for _, prov := range []string{"nibl", "xdcc_eu", "sunxdcc"} {
		if !providers[prov] {
			t.Errorf("missing provider %s in results", prov)
		}
	}
}

func TestGetSearchCacheByQuery_Empty(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	entries, err := s.GetSearchCacheByQuery("nonexistent")
	if err != nil {
		t.Fatalf("GetSearchCacheByQuery: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(entries))
	}
}

// TestGetSearchCacheByQuery_NoDeadlock verifies that GetSearchCacheByQuery
// does not deadlock even with limited connections. This is the regression test
// for the critical bug where getFresh() used nested queries.
func TestGetSearchCacheByQuery_NoDeadlock(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	now := time.Now()
	// Populate cache
	for i := 0; i < 5; i++ {
		entry := SearchCacheEntry{
			QueryKey:       "deadlock test",
			Provider:       fmt.Sprintf("provider_%d", i),
			PayloadJSON:    `[{"filename":"test.mkv"}]`,
			FetchedAt:      now,
			ExpiresAt:      now.Add(time.Hour),
			StaleExpiresAt: now.Add(24 * time.Hour),
		}
		_ = s.SetSearchCache(entry)
	}

	// Run concurrent GetSearchCacheByQuery - should not deadlock
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			entries, err := s.GetSearchCacheByQuery("deadlock test")
			done <- (err == nil && len(entries) == 5)
		}()
	}

	// If this times out, we have a deadlock
	timeout := time.After(5 * time.Second)
	for i := 0; i < 10; i++ {
		select {
		case ok := <-done:
			if !ok {
				t.Error("concurrent GetSearchCacheByQuery returned unexpected result")
			}
		case <-timeout:
			t.Fatal("DEADLOCK: GetSearchCacheByQuery timed out under concurrent access")
		}
	}
}

// ===========================================================================
// Search Presets
// ===========================================================================

func TestAddAndGetSearchPreset(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	p := SearchPreset{
		Name:        "Anime Weekly",
		Query:       "anime 1080p",
		FiltersJSON: `{"ext":"mkv"}`,
		IsDefault:   true,
	}

	id, err := s.AddSearchPreset(p)
	if err != nil {
		t.Fatalf("AddSearchPreset: %v", err)
	}

	got, err := s.GetSearchPreset(id)
	if err != nil {
		t.Fatalf("GetSearchPreset: %v", err)
	}
	if got == nil {
		t.Fatal("expected preset, got nil")
	}
	if got.Name != "Anime Weekly" {
		t.Errorf("expected name 'Anime Weekly', got %s", got.Name)
	}
	if !got.IsDefault {
		t.Errorf("expected is_default=true")
	}
}

func TestListSearchPresets(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	_, _ = s.AddSearchPreset(SearchPreset{Name: "Preset A", Query: "query a"})
	_, _ = s.AddSearchPreset(SearchPreset{Name: "Preset B", Query: "query b"})

	presets, err := s.ListSearchPresets()
	if err != nil {
		t.Fatalf("ListSearchPresets: %v", err)
	}
	if len(presets) != 2 {
		t.Errorf("expected 2 presets, got %d", len(presets))
	}
}

func TestUpdateSearchPreset(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddSearchPreset(SearchPreset{Name: "Old Name", Query: "old query"})
	err := s.UpdateSearchPreset(SearchPreset{ID: id, Name: "New Name", Query: "new query", FiltersJSON: "{}"})
	if err != nil {
		t.Fatalf("UpdateSearchPreset: %v", err)
	}

	got, _ := s.GetSearchPreset(id)
	if got.Name != "New Name" {
		t.Errorf("expected name 'New Name', got %s", got.Name)
	}
}

func TestDeleteSearchPreset(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddSearchPreset(SearchPreset{Name: "Del Me", Query: "delete me"})
	_ = s.DeleteSearchPreset(id)

	got, _ := s.GetSearchPreset(id)
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

func TestSetDefaultSearchPreset(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id1, _ := s.AddSearchPreset(SearchPreset{Name: "A", Query: "a", IsDefault: true})
	id2, _ := s.AddSearchPreset(SearchPreset{Name: "B", Query: "b"})

	err := s.SetDefaultSearchPreset(id2)
	if err != nil {
		t.Fatalf("SetDefaultSearchPreset: %v", err)
	}

	p1, _ := s.GetSearchPreset(id1)
	p2, _ := s.GetSearchPreset(id2)
	if p1.IsDefault {
		t.Errorf("expected preset A to no longer be default")
	}
	if !p2.IsDefault {
		t.Errorf("expected preset B to be default")
	}
}

// ===========================================================================
// Watchlists
// ===========================================================================

func TestAddAndGetWatchlist(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	w := Watchlist{
		Name:        "My Watchlist",
		Query:       "anime 1080p",
		FiltersJSON: `{"ext":"mkv"}`,
		Enabled:     true,
		AutoEnqueue: false,
	}

	id, err := s.AddWatchlist(w)
	if err != nil {
		t.Fatalf("AddWatchlist: %v", err)
	}

	got, err := s.GetWatchlist(id)
	if err != nil {
		t.Fatalf("GetWatchlist: %v", err)
	}
	if got == nil {
		t.Fatal("expected watchlist, got nil")
	}
	if got.Name != "My Watchlist" {
		t.Errorf("expected name 'My Watchlist', got %s", got.Name)
	}
	if !got.Enabled {
		t.Errorf("expected enabled=true")
	}
}

func TestListWatchlists(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	_, _ = s.AddWatchlist(Watchlist{Name: "WL1", Query: "query1"})
	_, _ = s.AddWatchlist(Watchlist{Name: "WL2", Query: "query2"})

	lists, err := s.ListWatchlists()
	if err != nil {
		t.Fatalf("ListWatchlists: %v", err)
	}
	if len(lists) != 2 {
		t.Errorf("expected 2 watchlists, got %d", len(lists))
	}
}

func TestUpdateWatchlist(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddWatchlist(Watchlist{Name: "Old", Query: "old", Enabled: true})
	err := s.UpdateWatchlist(Watchlist{ID: id, Name: "New", Query: "new", Enabled: false, AutoEnqueue: true})
	if err != nil {
		t.Fatalf("UpdateWatchlist: %v", err)
	}

	got, _ := s.GetWatchlist(id)
	if got.Name != "New" {
		t.Errorf("expected name 'New', got %s", got.Name)
	}
	if got.Enabled {
		t.Errorf("expected enabled=false")
	}
	if !got.AutoEnqueue {
		t.Errorf("expected auto_enqueue=true")
	}
}

func TestDeleteWatchlist(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddWatchlist(Watchlist{Name: "Del", Query: "delete"})
	_ = s.DeleteWatchlist(id)

	got, _ := s.GetWatchlist(id)
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

func TestSetWatchlistChecked(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.AddWatchlist(Watchlist{Name: "Check", Query: "check"})
	err := s.SetWatchlistChecked(id, "abc123")
	if err != nil {
		t.Fatalf("SetWatchlistChecked: %v", err)
	}

	w, _ := s.GetWatchlist(id)
	if w.LastMatchFingerprint != "abc123" {
		t.Errorf("expected fingerprint 'abc123', got %s", w.LastMatchFingerprint)
	}
	if w.LastCheckedAt == nil {
		t.Errorf("expected last_checked_at to be set")
	}
}

func TestGetEnabledWatchlists(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	_, _ = s.AddWatchlist(Watchlist{Name: "Enabled1", Query: "q1", Enabled: true})
	_, _ = s.AddWatchlist(Watchlist{Name: "Disabled", Query: "q2", Enabled: false})
	_, _ = s.AddWatchlist(Watchlist{Name: "Enabled2", Query: "q3", Enabled: true})

	enabled, err := s.GetEnabledWatchlists()
	if err != nil {
		t.Fatalf("GetEnabledWatchlists: %v", err)
	}
	if len(enabled) != 2 {
		t.Errorf("expected 2 enabled watchlists, got %d", len(enabled))
	}
}

// ===========================================================================
// Provider Stats
// ===========================================================================

func TestRecordAndGetProviderStats(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	now := time.Now()
	stats := ProviderStats{
		Provider:     "nibl",
		WindowStart:  now.Truncate(1 * time.Hour),
		WindowEnd:    now.Truncate(1 * time.Hour).Add(1 * time.Hour),
		Requests:     10,
		Successes:    8,
		Timeouts:     1,
		Failures:     1,
		AvgLatencyMs: 250.5,
		UpdatedAt:    now,
	}

	err := s.RecordProviderStats(stats)
	if err != nil {
		t.Fatalf("RecordProviderStats: %v", err)
	}

	since := now.Add(-2 * time.Hour)
	got, err := s.GetProviderStats("nibl", since)
	if err != nil {
		t.Fatalf("GetProviderStats: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 stats record, got %d", len(got))
	}
	if len(got) > 0 {
		if got[0].Requests != 10 {
			t.Errorf("expected 10 requests, got %d", got[0].Requests)
		}
	}
}

func TestGetAllProviderStats(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	now := time.Now()
	_ = s.RecordProviderStats(ProviderStats{
		Provider: "nibl", WindowStart: now.Add(-30 * time.Minute),
		WindowEnd: now, Requests: 5, Successes: 5,
	})
	_ = s.RecordProviderStats(ProviderStats{
		Provider: "xdcc_eu", WindowStart: now.Add(-30 * time.Minute),
		WindowEnd: now, Requests: 3, Successes: 3,
	})

	all, err := s.GetAllProviderStats(now.Add(-2 * time.Hour))
	if err != nil {
		t.Fatalf("GetAllProviderStats: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 providers, got %d", len(all))
	}
}

// ===========================================================================
// CurrentSchemaVersion
// ===========================================================================

func TestCurrentSchemaVersion(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	v, err := s.CurrentSchemaVersion()
	if err != nil {
		t.Fatalf("CurrentSchemaVersion: %v", err)
	}
	if v < 0 {
		t.Errorf("expected non-negative schema version, got %d", v)
	}
}

// ===========================================================================
// Export / Import
// ===========================================================================

func TestExportData_Empty(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	exp, err := s.ExportData()
	if err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if exp == nil {
		t.Fatal("expected non-nil export data")
	}
	if len(exp.Servers) != 0 {
		t.Errorf("expected empty servers, got %d", len(exp.Servers))
	}
}

func TestExportImportData(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	// Add some data
	_, _ = s.AddServer(ServerRecord{Address: "irc.export.net", Port: 6667, Status: "connected"})
	_, _ = s.AddWatchlist(Watchlist{Name: "Export WL", Query: "test", Enabled: true})
	_, _ = s.AddSearchPreset(SearchPreset{Name: "Export Preset", Query: "test"})

	exp, err := s.ExportData()
	if err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if len(exp.Servers) != 1 {
		t.Errorf("expected 1 server in export, got %d", len(exp.Servers))
	}
	if len(exp.Watchlists) != 1 {
		t.Errorf("expected 1 watchlist in export, got %d", len(exp.Watchlists))
	}

	// Import into fresh store
	s2 := newTestStore(t)
	defer closeStore(t, s2)

	err = s2.ImportData(exp)
	if err != nil {
		t.Fatalf("ImportData: %v", err)
	}

	// Verify imported data
	servers, _ := s2.ListServers()
	if len(servers) != 1 {
		t.Errorf("expected 1 server imported, got %d", len(servers))
	}
	if servers[0].Address != "irc.export.net" {
		t.Errorf("expected address 'irc.export.net', got %s", servers[0].Address)
	}

	wls, _ := s2.ListWatchlists()
	if len(wls) != 1 {
		t.Errorf("expected 1 watchlist imported, got %d", len(wls))
	}
}

// ===========================================================================
// Vacuum
// ===========================================================================

func TestVacuum(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	err := s.Vacuum()
	if err != nil {
		t.Fatalf("Vacuum: %v", err)
	}
}

// ===========================================================================
// Backup
// ===========================================================================

func TestBackupDatabase(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	err := s.BackupDatabase(backupPath)
	if err != nil {
		t.Fatalf("BackupDatabase: %v", err)
	}

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Errorf("backup file should exist at %s", backupPath)
	}
}

// ===========================================================================
// GetDownloadByBotMessage
// ===========================================================================

func TestGetDownloadByBotMessage(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "MyBot", ServerAddress: "irc.t.net", Channel: "#x",
		Filename: "f.mkv", FileSize: 1000, PackMessage: "xdcc send #42",
	})

	d, err := s.GetDownloadByBotMessage("MyBot", "xdcc send #42")
	if err != nil {
		t.Fatalf("GetDownloadByBotMessage: %v", err)
	}
	if d == nil {
		t.Fatal("expected download, got nil")
	}
	if d.ID != id {
		t.Errorf("expected id %d, got %d", id, d.ID)
	}

	// Not found
	missing, _ := s.GetDownloadByBotMessage("MyBot", "xdcc send #999")
	if missing != nil {
		t.Errorf("expected nil for non-matching message, got %+v", missing)
	}
}

// ===========================================================================
// RequeueDownload helper
// ===========================================================================

func TestRequeueDownload(t *testing.T) {
	s := newTestStore(t)
	defer closeStore(t, s)

	id, _ := s.EnqueueDownload(DownloadRecord{
		Bot: "Bot", ServerAddress: "irc.t.net", Channel: "#x", Filename: "f.mkv", FileSize: 1000,
	})
	_ = s.MarkDownloadCompleted(id, "", 0)

	err := s.RequeueDownload(id)
	if err != nil {
		t.Fatalf("RequeueDownload: %v", err)
	}

	d, _ := s.GetDownload(id)
	if d.Status != DownloadStatusQueued {
		t.Errorf("expected status 'queued', got %s", d.Status)
	}
}
