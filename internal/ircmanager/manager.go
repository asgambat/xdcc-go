package ircmanager

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/lrstanley/girc"
	"xdcc-go/internal/config"
	"xdcc-go/internal/store"
)

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

// Manager manages persistent IRC connections to multiple servers.
// Each server gets a dedicated connection that stays alive until explicitly
// disconnected. Auto-connect servers from the configuration are connected
// on Start(). Events are emitted via Subscribe() for SSE propagation.
type Manager struct {
	mu     sync.RWMutex
	store  store.Store
	cfg    *config.Config
	logger *log.Logger

	conns      map[int64]*managedConnection
	subscriber *subscriberHub

	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a new IRC connection manager.
func New(st store.Store, cfg *config.Config, logger *log.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		store:      st,
		cfg:        cfg,
		logger:     logger,
		conns:      make(map[int64]*managedConnection),
		subscriber: newSubscriberHub(),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// ---------------------------------------------------------------------------
// Lifecycle (Fase 3.2, 3.3)
// ---------------------------------------------------------------------------

// Start connects to all auto-connect servers from the configuration and joins
// their auto-join channels. It also connects to any servers marked auto_connect
// in the database that are not yet managed.
func (m *Manager) Start() error {
	// Connect default servers from config
	for _, sc := range m.cfg.IRC.DefaultServers {
		if !sc.AutoConnect {
			continue
		}

		// Check if already stored in DB
		servers, err := m.store.ListServers()
		if err != nil {
			m.logger.Printf("WARNING: listing servers failed: %v", err)
			continue
		}

		var existingID int64
		var found bool
		for _, s := range servers {
			if s.Address == sc.Address && s.Port == sc.Port {
				existingID = s.ID
				found = true
				break
			}
		}

		if !found {
			// Add to DB
			id, err := m.store.AddServer(store.ServerRecord{
				Address:     sc.Address,
				Port:        sc.Port,
				AutoConnect: true,
				Status:      "disconnected",
			})
			if err != nil {
				m.logger.Printf("WARNING: adding server %s to DB failed: %v", sc.Address, err)
				continue
			}
			existingID = id

			// Add channels to DB
			for _, cc := range sc.Channels {
				_, err := m.store.AddChannel(store.ChannelRecord{
					ServerID: existingID,
					Name:     cc.Name,
					AutoJoin: cc.AutoJoin,
					Joined:   false,
				})
				if err != nil {
					m.logger.Printf("WARNING: adding channel %s to DB failed: %v", cc.Name, err)
				}
			}
		}

		// Connect this server
		if err := m.ConnectServerByID(existingID); err != nil {
			m.logger.Printf("WARNING: connecting to %s failed: %v", sc.Address, err)
		}
	}

	// Also connect any DB servers marked auto_connect that aren't in config
	servers, err := m.store.ListServers()
	if err != nil {
		return fmt.Errorf("listing servers: %w", err)
	}

	m.mu.RLock()
	for _, s := range servers {
		if s.AutoConnect && s.Status == "disconnected" {
			if _, exists := m.conns[s.ID]; !exists {
				m.mu.RUnlock()
				if err := m.ConnectServerByID(s.ID); err != nil {
					m.logger.Printf("WARNING: connecting to server %s (id=%d) failed: %v", s.Address, s.ID, err)
				}
				m.mu.RLock()
			}
		}
	}
	m.mu.RUnlock()

	return nil
}

// Stop gracefully disconnects all managed connections.
func (m *Manager) Stop() {
	m.mu.RLock()
	ids := make([]int64, 0, len(m.conns))
	for id := range m.conns {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(sid int64) {
			defer wg.Done()
			m.DisconnectServer(sid)
		}(id)
	}
	wg.Wait()

	m.cancel()
}

// ---------------------------------------------------------------------------
// Subscribe to events (Fase 3.6)
// ---------------------------------------------------------------------------

// Subscribe returns a channel that receives state change events.
// The caller MUST consume from the channel, or the manager will block on
// event emission when the subscriber queue fills up.
func (m *Manager) Subscribe() chan Event {
	return m.subscriber.subscribe()
}

// Unsubscribe removes a previously subscribed channel.
func (m *Manager) Unsubscribe(ch chan Event) {
	m.subscriber.unsubscribe(ch)
}

// emitEvent sends an event to all subscribers (non-blocking).
func (m *Manager) emitEvent(evt Event) {
	evt.Timestamp = time.Now()
	m.subscriber.publish(evt)
}

// ---------------------------------------------------------------------------
// Public API (Fase 3.5)
// ---------------------------------------------------------------------------

// ConnectServerByID connects to an IRC server using its database ID.
// It loads server details from the store, including auto-join channels.
func (m *Manager) ConnectServerByID(serverID int64) error {
	srv, err := m.store.GetServer(serverID)
	if err != nil {
		return fmt.Errorf("fetching server %d: %w", serverID, err)
	}
	if srv == nil {
		return fmt.Errorf("server %d not found", serverID)
	}
	return m.ConnectServer(srv)
}

// ConnectServer connects to an IRC server with the given details.
// If the server is already connected, it returns nil.
func (m *Manager) ConnectServer(srv *store.ServerRecord) error {
	m.mu.Lock()
	// Check if already connected or connecting
	if existing, ok := m.conns[srv.ID]; ok {
		m.mu.Unlock()
		if existing.Status() == "connected" {
			// Already connected — return without modification
			return nil
		}
		// Cancel the old stale connection outside critical section
		existing.cancel()
		// Wait briefly for cleanup
		time.Sleep(10 * time.Millisecond)
	} else {
		m.mu.Unlock()
	}

	// Create new managed connection
	conn := &managedConnection{
		id:        srv.ID,
		address:   srv.Address,
		port:      srv.Port,
		nickname:  m.cfg.IRC.Nickname,
		manager:   m,
		joinedChs: make(map[string]string),
		status:    "connecting",
	}

	// Load auto-join channels from DB
	channels, err := m.store.GetChannelsByServer(srv.ID)
	if err == nil {
		for _, ch := range channels {
			if ch.AutoJoin {
				conn.autoJoinChs = append(conn.autoJoinChs, ch.Name)
			}
		}
	}

	m.mu.Lock()
	m.conns[srv.ID] = conn
	m.mu.Unlock()

	// Update DB status to 'connecting' (not 'connected' yet)
	if err := m.store.SetServerStatus(srv.ID, "connecting"); err != nil {
		m.logger.Printf("WARNING: updating server status in DB failed: %v", err)
	}

	// Start connection in background
	conn.ctx, conn.cancel = context.WithCancel(m.ctx)
	go conn.run()

	return nil
}

// DisconnectServer disconnects from an IRC server by its ID.
func (m *Manager) DisconnectServer(serverID int64) error {
	m.mu.Lock()
	conn, ok := m.conns[serverID]
	if ok {
		delete(m.conns, serverID)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("server %d is not managed", serverID)
	}

	conn.disconnect()
	if err := m.store.SetServerStatus(serverID, "disconnected"); err != nil {
		m.logger.Printf("WARNING: updating server status in DB failed: %v", err)
	}
	return nil
}

// JoinChannel joins a channel on a specific server.
func (m *Manager) JoinChannel(serverID int64, channel string) error {
	m.mu.RLock()
	conn, ok := m.conns[serverID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("server %d is not connected", serverID)
	}
	return conn.joinChannel(channel)
}

// LeaveChannel leaves a channel on a specific server.
func (m *Manager) LeaveChannel(serverID int64, channel string) error {
	m.mu.RLock()
	conn, ok := m.conns[serverID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("server %d is not connected", serverID)
	}
	return conn.leaveChannel(channel)
}

// GetServers returns the list of all known IRC servers with their status.
func (m *Manager) GetServers() []store.ServerRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	servers, err := m.store.ListServers()
	if err != nil {
		m.logger.Printf("WARNING: listing servers failed: %v", err)
		return nil
	}

	// Overlay live status from managed connections
	for i, s := range servers {
		if conn, ok := m.conns[s.ID]; ok {
			servers[i].Status = conn.Status()
		}
	}
	return servers
}

// GetChannels returns the list of channels for a specific server.
func (m *Manager) GetChannels(serverID int64) []store.ChannelRecord {
	channels, err := m.store.GetChannelsByServer(serverID)
	if err != nil {
		m.logger.Printf("WARNING: listing channels for server %d failed: %v", serverID, err)
		return nil
	}

	// Overlay join status and topic from live connection
	m.mu.RLock()
	conn, ok := m.conns[serverID]
	m.mu.RUnlock()

	if ok {
		conn.mu.RLock()
		for i, ch := range channels {
			if topic, joined := conn.joinedChs[ch.Name]; joined {
				channels[i].Joined = true
				channels[i].Topic = topic
			}
		}
		conn.mu.RUnlock()
	}

	return channels
}

// GetChannelTopic returns the topic for a specific channel.
func (m *Manager) GetChannelTopic(serverID int64, channel string) (string, error) {
	m.mu.RLock()
	conn, ok := m.conns[serverID]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("server %d is not connected", serverID)
	}

	conn.mu.RLock()
	topic, joined := conn.joinedChs[channel]
	conn.mu.RUnlock()
	if !joined {
		return "", fmt.Errorf("not joined to channel %s", channel)
	}
	return topic, nil
}

// ---------------------------------------------------------------------------
// managedConnection
// ---------------------------------------------------------------------------

// managedConnection manages a single persistent IRC connection.
// It handles reconnection with exponential backoff and tracks channel state.
type managedConnection struct {
	id          int64
	address     string
	port        int
	nickname    string
	autoJoinChs []string // channels to auto-join on (re)connect

	mu         sync.RWMutex
	status     string            // disconnected, connecting, connected, reconnecting
	joinedChs  map[string]string // channel name -> topic
	retryCount int
	backoffIdx int

	irc *girc.Client

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	manager *Manager
}

func (mc *managedConnection) Status() string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.status
}

func (mc *managedConnection) setStatus(s string) {
	mc.mu.Lock()
	mc.status = s
	mc.mu.Unlock()
}

// connectResult represents the outcome of a connection attempt.
type connectResult int

const (
	connectResultExplicitCancel connectResult = iota // User requested disconnect
	connectResultInitialFailure                      // Failed on first attempt
	connectResultDropped                             // Connection dropped after being established
)

// run is the main loop for a managed connection. It connects, waits for
// disconnection, then reconnects with exponential backoff unless explicitly cancelled.
func (mc *managedConnection) run() {
	mc.done = make(chan struct{})
	defer close(mc.done)

	for {
		result := mc.connect()
		if mc.ctx.Err() != nil {
			// Context cancelled — server was explicitly disconnected
			mc.setStatus("disconnected")
			_ = mc.manager.store.SetServerStatus(mc.id, "disconnected")
			mc.manager.emitEvent(Event{
				Type:       EventServerDisconnected,
				ServerID:   mc.id,
				ServerAddr: mc.address,
			})
			return
		}

		// Handle result based on disconnect reason
		switch result {
		case connectResultExplicitCancel:
			// User explicitly disconnected — stop reconnecting
			mc.setStatus("disconnected")
			_ = mc.manager.store.SetServerStatus(mc.id, "disconnected")
			mc.manager.emitEvent(Event{
				Type:       EventServerDisconnected,
				ServerID:   mc.id,
				ServerAddr: mc.address,
			})
			return
		case connectResultInitialFailure, connectResultDropped:
			// Automatic reconnect for failures and drops
			mc.manager.logger.Printf("IRC connection to %s lost, reconnecting...", mc.address)
			mc.manager.emitEvent(Event{
				Type:       EventServerDisconnected,
				ServerID:   mc.id,
				ServerAddr: mc.address,
			})
			if !mc.reconnectBackoff() {
				return // context cancelled during backoff
			}
		}
	}
}

// connect establishes a single IRC connection and returns the disconnect reason.
func (mc *managedConnection) connect() connectResult {
	// Clear stale channel state before new connection attempt
	mc.mu.Lock()
	mc.joinedChs = make(map[string]string)
	mc.irc = nil
	mc.mu.Unlock()

	nick := mc.nickname + randomSuffix(3)
	mc.setStatus("connecting")

	mc.manager.logger.Printf("connecting to %s:%d as '%s'", mc.address, mc.port, nick)

	client := girc.New(girc.Config{
		Server:      mc.address,
		Port:        mc.port,
		Nick:        nick,
		User:        nick,
		Name:        nick,
		PingDelay:   30 * time.Second,
		PingTimeout: 60 * time.Second,
	})

	connected := make(chan struct{})
	disconnected := make(chan error, 1)

	// Register handlers
	client.Handlers.Add(girc.CONNECTED, func(cl *girc.Client, e girc.Event) {
		mc.mu.Lock()
		mc.status = "connected"
		mc.retryCount = 0
		mc.backoffIdx = 0
		mc.irc = cl
		mc.mu.Unlock()

		mc.manager.logger.Printf("connected to %s:%d", mc.address, mc.port)

		// Update DB
		if err := mc.manager.store.SetServerConnected(mc.id); err != nil {
			mc.manager.logger.Printf("WARNING: updating server %d status failed: %v", mc.id, err)
		}

		// Emit event
		mc.manager.emitEvent(Event{
			Type:       EventServerConnected,
			ServerID:   mc.id,
			ServerAddr: mc.address,
		})

		// Auto-join channels
		for _, ch := range mc.autoJoinChs {
			cl.Cmd.Join(ch)
		}

		close(connected)
	})

	client.Handlers.Add(girc.JOIN, func(cl *girc.Client, e girc.Event) {
		if e.Source == nil || !isOwnNick(e.Source.Name, cl.GetNick()) {
			return
		}
		ch := normalizeChannel(e.Params[0])
		mc.mu.Lock()
		mc.joinedChs[ch] = "" // topic will be updated by TOPIC event
		mc.mu.Unlock()

		// Update DB
		channels, err := mc.manager.store.GetChannelsByServerAndName(mc.id, ch)
		if err == nil && channels != nil {
			_ = mc.manager.store.SetChannelJoined(channels.ID, true)
		}

		mc.manager.emitEvent(Event{
			Type:       EventChannelJoined,
			ServerID:   mc.id,
			ServerAddr: mc.address,
			Channel:    ch,
		})
	})

	client.Handlers.Add(girc.KICK, func(cl *girc.Client, e girc.Event) {
		if len(e.Params) < 2 {
			return
		}
		if !isOwnNick(e.Params[1], cl.GetNick()) {
			return
		}
		ch := normalizeChannel(e.Params[0])
		mc.mu.Lock()
		delete(mc.joinedChs, ch)
		mc.mu.Unlock()

		channels, err := mc.manager.store.GetChannelsByServerAndName(mc.id, ch)
		if err == nil && channels != nil {
			_ = mc.manager.store.SetChannelJoined(channels.ID, false)
		}

		mc.manager.emitEvent(Event{
			Type:       EventChannelLeft,
			ServerID:   mc.id,
			ServerAddr: mc.address,
			Channel:    ch,
		})
	})

	client.Handlers.Add(girc.PART, func(cl *girc.Client, e girc.Event) {
		if e.Source == nil || !isOwnNick(e.Source.Name, cl.GetNick()) {
			return
		}
		ch := normalizeChannel(e.Params[0])
		mc.mu.Lock()
		delete(mc.joinedChs, ch)
		mc.mu.Unlock()

		channels, err := mc.manager.store.GetChannelsByServerAndName(mc.id, ch)
		if err == nil && channels != nil {
			_ = mc.manager.store.SetChannelJoined(channels.ID, false)
		}

		mc.manager.emitEvent(Event{
			Type:       EventChannelLeft,
			ServerID:   mc.id,
			ServerAddr: mc.address,
			Channel:    ch,
		})
	})

	client.Handlers.Add(girc.TOPIC, func(cl *girc.Client, e girc.Event) {
		if len(e.Params) < 1 {
			return
		}
		ch := normalizeChannel(e.Params[0])
		topic := e.Last()
		mc.mu.Lock()
		mc.joinedChs[ch] = topic
		mc.mu.Unlock()

		channels, err := mc.manager.store.GetChannelsByServerAndName(mc.id, ch)
		if err == nil && channels != nil {
			_ = mc.manager.store.UpdateChannelTopic(channels.ID, topic)
		}

		mc.manager.emitEvent(Event{
			Type:       EventChannelTopicUpdated,
			ServerID:   mc.id,
			ServerAddr: mc.address,
			Channel:    ch,
			Topic:      topic,
		})
	})

	client.Handlers.Add(girc.RPL_TOPIC, func(cl *girc.Client, e girc.Event) {
		if len(e.Params) < 3 {
			return
		}
		ch := normalizeChannel(e.Params[1])
		topic := e.Params[len(e.Params)-1]
		mc.mu.Lock()
		mc.joinedChs[ch] = topic
		mc.mu.Unlock()
	})

	client.Handlers.Add(girc.ERROR, func(cl *girc.Client, e girc.Event) {
		mc.manager.logger.Printf("IRC error on %s: %s", mc.address, e.Last())
	})

	// Start connection in a goroutine; when irc.Connect() returns,
	// the connection has been lost (either due to error or explicit Close).
	go func() {
		err := client.Connect()
		disconnected <- err
		close(disconnected)
	}()

	// Phase 1: Wait for CONNECTED event or immediate failure
	select {
	case <-mc.ctx.Done():
		client.Close()
		<-disconnected // drain
		return connectResultExplicitCancel
	case <-connected:
		// Connected successfully — proceed to Phase 2
	case err := <-disconnected:
		// Connection failed on first attempt
		mc.manager.logger.Printf("connection to %s failed: %v", mc.address, err)
		_ = mc.manager.store.IncrementServerRetry(mc.id)
		return connectResultInitialFailure
	}

	// Phase 2: Connection is established. Wait for disconnection or cancellation.
	select {
	case <-mc.ctx.Done():
		// Explicit disconnect requested — send QUIT, close, then drain
		client.Close()
		// Drain the disconnected channel (buffered with capacity 1, so drain succeeds)
		<-disconnected
		return connectResultExplicitCancel
	case err := <-disconnected:
		// Connection dropped — will trigger reconnect in run()
		if err != nil {
			mc.manager.logger.Printf("connection to %s lost: %v", mc.address, err)
		}
		return connectResultDropped
	}
}

// reconnectBackoff implements exponential backoff (Fase 3.4).
// Returns false if the context was cancelled.
func (mc *managedConnection) reconnectBackoff() bool {
	mc.mu.Lock()
	mc.status = "reconnecting"
	mc.retryCount++
	idx := mc.backoffIdx
	if idx < 5 {
		mc.backoffIdx++
	}
	mc.mu.Unlock()

	// Notify the manager to update DB
	_ = mc.manager.store.SetServerStatus(mc.id, "reconnecting")

	mc.manager.emitEvent(Event{
		Type:       EventServerReconnecting,
		ServerID:   mc.id,
		ServerAddr: mc.address,
	})

	// Calculate backoff delay
	delays := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second}
	var delay time.Duration
	if idx < len(delays) {
		delay = delays[idx]
	} else {
		delay = 1 * time.Hour // after 5 failures, retry every hour
	}

	mc.manager.logger.Printf("reconnecting to %s in %v (attempt %d)", mc.address, delay, mc.retryCount)

	select {
	case <-mc.ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

// disconnect tears down the connection gracefully.
func (mc *managedConnection) disconnect() {
	mc.setStatus("disconnected")
	if mc.cancel != nil {
		mc.cancel()
	}
}

// joinChannel sends a JOIN command for the given channel.
func (mc *managedConnection) joinChannel(channel string) error {
	mc.mu.RLock()
	client := mc.irc
	mc.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected")
	}

	// Normalize channel name
	channel = normalizeChannel(channel)

	// Persist to DB: create or update channel record
	existingCh, err := mc.manager.store.GetChannelsByServerAndName(mc.id, channel)
	if err != nil || existingCh == nil {
		// Channel doesn't exist — create it
		_, err = mc.manager.store.AddChannel(store.ChannelRecord{
			ServerID: mc.id,
			Name:     channel,
			AutoJoin: true,
			Joined:   false, // Will be set to true by JOIN handler
		})
		if err != nil {
			mc.manager.logger.Printf("WARNING: failed to add channel %s to DB: %v", channel, err)
		}
	} else {
		// Channel exists — update auto_join to true
		existingCh.AutoJoin = true
		if err := mc.manager.store.UpdateChannel(*existingCh); err != nil {
			mc.manager.logger.Printf("WARNING: failed to update channel %s in DB: %v", channel, err)
		}
	}

	// Send JOIN command to IRC
	client.Cmd.Join(channel)

	// Add to auto-join for reconnection (in-memory)
	mc.mu.Lock()
	found := false
	for _, ch := range mc.autoJoinChs {
		if ch == channel {
			found = true
			break
		}
	}
	if !found {
		mc.autoJoinChs = append(mc.autoJoinChs, channel)
	}
	mc.mu.Unlock()

	return nil
}

// leaveChannel sends a PART command for the given channel.
func (mc *managedConnection) leaveChannel(channel string) error {
	mc.mu.RLock()
	client := mc.irc
	mc.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected")
	}

	// Normalize channel name
	channel = normalizeChannel(channel)

	// Update DB: set auto_join=false and joined=false
	existingCh, err := mc.manager.store.GetChannelsByServerAndName(mc.id, channel)
	if err == nil && existingCh != nil {
		existingCh.AutoJoin = false
		existingCh.Joined = false
		if err := mc.manager.store.UpdateChannel(*existingCh); err != nil {
			mc.manager.logger.Printf("WARNING: failed to update channel %s in DB: %v", channel, err)
		}
	}

	// Send PART command to IRC
	client.Cmd.Part(channel)

	// Remove from auto-join list (in-memory)
	mc.mu.Lock()
	for i, ch := range mc.autoJoinChs {
		if ch == channel {
			mc.autoJoinChs = append(mc.autoJoinChs[:i], mc.autoJoinChs[i+1:]...)
			break
		}
	}
	mc.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// subscriberHub — manages event subscribers (Fase 3.6)
// ---------------------------------------------------------------------------

type subscriberHub struct {
	mu          sync.RWMutex
	subscribers []chan Event
}

func newSubscriberHub() *subscriberHub {
	return &subscriberHub{}
}

func (h *subscriberHub) subscribe() chan Event {
	ch := make(chan Event, 256)
	h.mu.Lock()
	h.subscribers = append(h.subscribers, ch)
	h.mu.Unlock()
	return ch
}

func (h *subscriberHub) unsubscribe(ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, s := range h.subscribers {
		if s == ch {
			h.subscribers = append(h.subscribers[:i], h.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

func (h *subscriberHub) publish(evt Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subscribers {
		select {
		case ch <- evt:
		default:
			// Drop event if subscriber is not consuming fast enough
		}
	}
}
