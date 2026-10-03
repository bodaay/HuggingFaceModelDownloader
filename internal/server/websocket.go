// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// isAllowedOrigin decides whether a browser request may reach the server; it
// guards both the REST API (corsMiddleware) and WebSocket upgrades. Without it
// any web page the user visits could drive a locally-bound server: browsers
// don't enforce CORS on WebSocket handshakes at all, and CORS on fetch only
// hides the response — a cross-site "simple" POST/DELETE still executes.
// Policy:
//   - No Origin header (native clients such as curl/websocat): allow.
//   - Same-origin (Origin host == request host): allow.
//   - Otherwise: allow only if the origin is in AllowedOrigins (or it is "*").
//
// With no AllowedOrigins configured this is default-deny for cross-origin,
// which matches the safe expectation for a tool bound to localhost/0.0.0.0.
func (s *Server) isAllowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser client; no Origin to forge or hijack.
		return true
	}
	if u, err := url.Parse(origin); err == nil && u.Host != "" && u.Host == r.Host {
		return true
	}
	for _, o := range s.config.AllowedOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}

// WSMessage represents a message sent over WebSocket.
type WSMessage struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// WSClient represents a connected WebSocket client.
type WSClient struct {
	conn      *websocket.Conn
	send      chan []byte
	hub       *WSHub
	closeOnce sync.Once
	closed    bool
	mu        sync.Mutex
}

// closeSend closes the client's send channel exactly once and marks the client
// closed. Both the hub's unregister path and its slow-client eviction path call
// this; sync.Once makes a double close (which would panic) impossible. The
// closed flag is set under c.mu so sendInitialState, which sends under the same
// lock, can never write to an already-closed channel.
func (c *WSClient) closeSend() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.send)
	})
}

// WSHub manages WebSocket clients and broadcasts.
type WSHub struct {
	clients    map[*WSClient]bool
	broadcast  chan []byte
	register   chan *WSClient
	unregister chan *WSClient
	mu         sync.RWMutex
}

// NewWSHub creates a new WebSocket hub.
func NewWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[*WSClient]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
	}
}

// Run starts the hub's main loop.
func (h *WSHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			count := len(h.clients)
			h.mu.Unlock()
			log.Printf("[WS] Client connected (%d total)", count)

		case client := <-h.unregister:
			h.mu.Lock()
			count := len(h.clients)
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.closeSend()
				count = len(h.clients)
			}
			h.mu.Unlock()
			log.Printf("[WS] Client disconnected (%d total)", count)

		case message := <-h.broadcast:
			// Take the write lock: a full send buffer means we must evict the
			// client, which mutates h.clients. Iterating + deleting under only
			// RLock (the previous behaviour) is a concurrent map write that can
			// panic and tear down the whole hub. Broadcasts come from a single
			// goroutine, so the extra exclusivity costs nothing in practice.
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client's buffer is full: drop it. closeSend is
					// once-guarded so the later unregister can't double close.
					delete(h.clients, client)
					client.closeSend()
				}
			}
			h.mu.Unlock()
		}
	}
}

// Broadcast sends a message to all connected clients.
func (h *WSHub) Broadcast(msgType string, data any) {
	msg := WSMessage{
		Type: msgType,
		Data: data,
	}
	
	jsonData, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[WS] Failed to marshal message: %v", err)
		return
	}

	select {
	case h.broadcast <- jsonData:
	default:
		log.Printf("[WS] Broadcast channel full, dropping message")
	}
}

// BroadcastJob sends a job update to all clients.
func (h *WSHub) BroadcastJob(job *Job) {
	h.Broadcast("job_update", job)
}

// BroadcastEvent sends a progress event to all clients.
func (h *WSHub) BroadcastEvent(event any) {
	h.Broadcast("event", event)
}

// ClientCount returns the number of connected clients.
func (h *WSHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// handleWebSocket handles WebSocket connections.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Upgrade failed: %v", err)
		return
	}

	client := &WSClient{
		conn: conn,
		send: make(chan []byte, 256),
		hub:  s.wsHub,
	}

	s.wsHub.register <- client

	// Start read/write pumps
	go client.writePump()
	go client.readPump()

	// Send initial state
	s.sendInitialState(client)
}

// sendInitialState sends current job state to newly connected client.
func (s *Server) sendInitialState(client *WSClient) {
	jobs := s.jobs.ListJobs()
	
	msg := WSMessage{
		Type: "init",
		Data: map[string]any{
			"jobs":    jobs,
			"version": s.version(),
		},
	}
	
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if !client.closed {
		select {
		case client.send <- data:
		default:
		}
	}
}

// writePump pumps messages from the hub to the WebSocket connection.
func (c *WSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Batch any queued messages
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte("\n"))
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump pumps messages from the WebSocket connection to the hub.
func (c *WSClient) readPump() {
	defer func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512 * 1024) // 512KB
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WS] Read error: %v", err)
			}
			break
		}

		// Handle incoming messages (for future use)
		_ = message
	}
}

