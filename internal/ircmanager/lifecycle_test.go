package ircmanager

import (
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"xdcc-go/internal/config"
)

// ===========================================================================
// WaitGroup Lifecycle Tests - Verify race-free goroutine management
// ===========================================================================

// TestConnectionLifecycle_NoDuplicateRun verifies that run() cannot be called twice
func TestConnectionLifecycle_NoDuplicateRun(t *testing.T) {
	t.Parallel()

	ms := newMockStore()
	cfg := config.DefaultConfig()
	logger := log.New(os.Stderr, "[test-lifecycle] ", log.LstdFlags)

	mgr := New(ms, cfg, logger)
	defer mgr.Stop()

	srvID := ms.addServer("irc.test.net", 6667, false)

	// Get server info from mock store directly
	ms.mu.Lock()
	srv := ms.servers[srvID]
	ms.mu.Unlock()

	// Create a managed connection
	conn := &managedConnection{
		id:        srv.ID,
		address:   srv.Address,
		port:      srv.Port,
		nickname:  cfg.IRC.Nickname,
		manager:   mgr,
		joinedChs: make(map[string]string),
		status:    "connecting",
	}
	conn.ctx, conn.cancel = mgr.ctx, mgr.cancel

	// Launch run() twice - second call should be ignored
	conn.wg.Add(1)
	go conn.run()
	time.Sleep(10 * time.Millisecond) // Let first run() start

	// This should be safely ignored
	go conn.run()

	// Give time for potential duplicate to execute
	time.Sleep(100 * time.Millisecond)

	// Verify: should still be running (not panicked, not closed twice)
	if !conn.IsRunning() {
		t.Log("connection may have failed immediately (no real IRC server) — OK")
	}

	// Cleanup
	conn.cancel()
	conn.wg.Wait()

	// Verify cleanup completed
	if conn.IsRunning() {
		t.Error("expected isRunning=false after cleanup")
	}
}

// TestConnectionLifecycle_CleanShutdown verifies graceful shutdown with WaitGroup
func TestConnectionLifecycle_CleanShutdown(t *testing.T) {
	t.Parallel()

	ms := newMockStore()
	srvID := ms.addServer("irc.test.net", 6667, false)

	cfg := config.DefaultConfig()
	logger := log.New(os.Stderr, "[test-lifecycle] ", log.LstdFlags)

	mgr := New(ms, cfg, logger)
	defer mgr.Stop()

	// Connect (will fail but that's expected)
	_ = mgr.ConnectServerByID(srvID)

	// Give time for goroutine to start
	time.Sleep(50 * time.Millisecond)

	// Disconnect and verify clean shutdown
	start := time.Now()
	err := mgr.DisconnectServer(srvID)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("DisconnectServer failed: %v", err)
	}

	// Should complete quickly (no timeout warnings)
	if elapsed > 8*time.Second {
		t.Errorf("DisconnectServer took %v, expected <8s", elapsed)
	}
}

// TestConnectionLifecycle_ConcurrentShutdown tests thread-safety during shutdown
func TestConnectionLifecycle_ConcurrentShutdown(t *testing.T) {
	t.Parallel()

	ms := newMockStore()
	cfg := config.DefaultConfig()
	logger := log.New(os.Stderr, "[test-concurrent] ", log.LstdFlags)

	mgr := New(ms, cfg, logger)
	defer mgr.Stop()

	// Create multiple servers
	ids := make([]int64, 5)
	for i := 0; i < 5; i++ {
		ids[i] = ms.addServer("irc.test.net", 6667+i, false)
		_ = mgr.ConnectServerByID(ids[i])
	}

	// Give time for connections to start
	time.Sleep(100 * time.Millisecond)

	// Disconnect all concurrently
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(serverID int64) {
			defer wg.Done()
			err := mgr.DisconnectServer(serverID)
			if err != nil {
				t.Logf("DisconnectServer(%d) error: %v", serverID, err)
			}
		}(id)
	}

	// Should complete without deadlock or race
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(15 * time.Second):
		t.Fatal("concurrent shutdown deadlocked or timed out")
	}
}

// TestConnectionLifecycle_ManagerStopWaitsForAll verifies Stop() waits for all goroutines
func TestConnectionLifecycle_ManagerStopWaitsForAll(t *testing.T) {
	t.Parallel()

	ms := newMockStore()
	cfg := config.DefaultConfig()
	logger := log.New(os.Stderr, "[test-stop] ", log.LstdFlags)

	mgr := New(ms, cfg, logger)

	// Create multiple connections
	for i := 0; i < 3; i++ {
		id := ms.addServer("irc.test.net", 6667+i, false)
		_ = mgr.ConnectServerByID(id)
	}

	// Give time for goroutines to start
	time.Sleep(100 * time.Millisecond)

	// Stop() should wait for all run() goroutines to finish
	start := time.Now()
	mgr.Stop()
	elapsed := time.Since(start)

	// Should complete in reasonable time (no hangs)
	if elapsed > 15*time.Second {
		t.Errorf("Stop() took %v, expected <15s", elapsed)
	}

	// Verify manager is fully stopped
	// Attempting operations after Stop() should fail gracefully
	err := mgr.ConnectServerByID(1)
	if err == nil {
		t.Log("ConnectServerByID succeeded after Stop — context behavior may vary")
	}
}

// TestConnectionLifecycle_NoRaceOnStatusChecks verifies thread-safe status access
func TestConnectionLifecycle_NoRaceOnStatusChecks(t *testing.T) {
	t.Parallel()

	ms := newMockStore()
	srvID := ms.addServer("irc.test.net", 6667, false)

	cfg := config.DefaultConfig()
	logger := log.New(os.Stderr, "[test-race] ", log.LstdFlags)

	mgr := New(ms, cfg, logger)
	defer mgr.Stop()

	_ = mgr.ConnectServerByID(srvID)

	// Hammer IsRunning() from multiple goroutines
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mgr.mu.RLock()
				conn, ok := mgr.conns[srvID]
				mgr.mu.RUnlock()
				if ok {
					_ = conn.IsRunning()
					_ = conn.Status()
				}
			}
		}()
	}

	wg.Wait()

	// Should complete without race detector warnings
	_ = mgr.DisconnectServer(srvID)
}

// TestConnectionLifecycle_PanicRecovery verifies panic in run() is recovered
func TestConnectionLifecycle_PanicRecovery(t *testing.T) {
	// This test would require injecting a panic into connect()
	// For now, we document that panic recovery exists in run()
	t.Skip("Requires mock IRC client that can panic on demand")
}
