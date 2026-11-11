// Package dmdatasim is a fake DMDATA.JP API v2 server (Socket Start,
// Socket Close, WebSocket v2) for tests and local verification, playing
// the role internal/wnisim used to play for WNI.
//
// It models the parts of the real service the client must get right:
// API-key auth, single-use tickets, the dmdata.v2 subprotocol, the
// start/ping/data/error message shapes, pingId-matched pongs, and
// connection slots freed by Socket Close or disconnect.
package dmdatasim

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Server is a running fake. Create with New, stop with Close.
type Server struct {
	// HTTP is the underlying test server; its URL is the API base.
	HTTP *httptest.Server

	apiKey       string
	pingInterval time.Duration
	maxSockets   int
	listPageSize int
	// contracts, if non-nil, limits which classifications are granted.
	contracts []string

	mu      sync.Mutex
	nextID  int
	tickets map[string]int // unused ticket -> socket id
	sockets map[int]*socket
	// Closed records socket IDs released via Socket Close, in order.
	closed []int
	starts []StartRequest
	pongs  []string

	connected chan int
}

// StartRequest is a Socket Start body the fake received.
type StartRequest struct {
	Classifications []string `json:"classifications"`
	Types           []string `json:"types"`
	Test            string   `json:"test"`
	AppName         string   `json:"appName"`
	FormatMode      string   `json:"formatMode"`
}

type socket struct {
	id  int
	req StartRequest
	// opened is set once a WebSocket connected on this socket's ticket;
	// before that Socket List reports it "waiting", after it "open" (even
	// after an unclean disconnect, until Socket Close).
	opened  bool
	start   time.Time
	conn    *websocket.Conn
	cancel  context.CancelFunc
	lastPID string
	ponged  bool
	writeMu sync.Mutex
}

// Option configures a Server.
type Option func(*Server)

// WithPingInterval sets how often the fake pings each connection
// (default: never, so tests drive pings explicitly via Ping).
func WithPingInterval(d time.Duration) Option { return func(s *Server) { s.pingInterval = d } }

// WithMaxSockets caps simultaneous sockets (default 2); Socket Start beyond
// it answers 409 like the real service.
func WithMaxSockets(n int) Option { return func(s *Server) { s.maxSockets = n } }

// WithContracts limits the account's contracted classifications (default:
// everything requested is granted). Socket Start answers 402 when none of
// the requested ones is contracted and otherwise grants only those that
// are, mirroring how the real service reports granted classifications.
func WithContracts(classifications ...string) Option {
	return func(s *Server) { s.contracts = append([]string{}, classifications...) }
}

// WithListPageSize caps Socket List pages below the client's limit, to
// exercise nextToken pagination.
func WithListPageSize(n int) Option { return func(s *Server) { s.listPageSize = n } }

// New starts a fake that accepts apiKey.
func New(apiKey string, opts ...Option) *Server {
	s := &Server{
		apiKey:     apiKey,
		maxSockets: 2,
		nextID:     1,
		tickets:    map[string]int{},
		sockets:    map[int]*socket{},
		connected:  make(chan int, 16),
	}
	for _, o := range opts {
		o(s)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/socket", s.handleStart)
	mux.HandleFunc("GET /v2/socket", s.handleList)
	mux.HandleFunc("DELETE /v2/socket/{id}", s.handleClose)
	mux.HandleFunc("GET /v2/websocket", s.handleWebSocket)
	s.HTTP = httptest.NewServer(mux)
	return s
}

// URL is the API base to point a client at.
func (s *Server) URL() string { return s.HTTP.URL }

// Close shuts the fake down, dropping every connection.
func (s *Server) Close() {
	s.mu.Lock()
	for _, sk := range s.sockets {
		if sk.cancel != nil {
			sk.cancel()
		}
	}
	s.mu.Unlock()
	s.HTTP.CloseClientConnections()
	s.HTTP.Close()
}

// Connected delivers the socket ID of each WebSocket that finished its
// handshake (after the start message was sent).
func (s *Server) Connected() <-chan int { return s.connected }

// ClosedSockets returns socket IDs released via Socket Close.
func (s *Server) ClosedSockets() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.closed...)
}

// OpenSockets returns how many connection slots are currently held.
func (s *Server) OpenSockets() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sockets)
}

// StartRequests returns every Socket Start body received.
func (s *Server) StartRequests() []StartRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]StartRequest(nil), s.starts...)
}

// Pongs returns every pingId the fake got back in a pong.
func (s *Server) Pongs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.pongs...)
}

func (s *Server) authorized(r *http.Request) bool {
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(s.apiKey+":"))
	return r.Header.Get("Authorization") == want
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"responseId":   randHex(8),
		"responseTime": time.Now().UTC().Format(time.RFC3339Nano),
		"status":       "error",
		"error":        map[string]any{"message": msg, "code": status},
	})
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeAPIError(w, http.StatusUnauthorized, "Unauthorized.")
		return
	}
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "The body of the request is not json.")
		return
	}
	if len(req.Classifications) == 0 {
		writeAPIError(w, http.StatusBadRequest, "At least one element of `classifications` is required.")
		return
	}
	if s.contracts != nil {
		var granted []string
		for _, c := range req.Classifications {
			if slices.Contains(s.contracts, c) {
				granted = append(granted, c)
			}
		}
		if len(granted) == 0 {
			writeAPIError(w, http.StatusPaymentRequired, "No contract.")
			return
		}
		req.Classifications = granted
	}
	if req.Test == "" {
		req.Test = "no"
	}
	if req.FormatMode == "" {
		req.FormatMode = "raw"
	}

	s.mu.Lock()
	if len(s.sockets) >= s.maxSockets {
		s.mu.Unlock()
		writeAPIError(w, http.StatusConflict, "The maximum number of simultaneous connections is full.")
		return
	}
	id := s.nextID
	s.nextID++
	ticket := "Tik" + randHex(16)
	s.tickets[ticket] = id
	s.sockets[id] = &socket{id: id, req: req, start: time.Now()}
	s.starts = append(s.starts, req)
	s.mu.Unlock()

	wsURL := "ws" + strings.TrimPrefix(s.HTTP.URL, "http") + "/v2/websocket?ticket=" + ticket
	formats := []string{"xml", "a/n", "binary"}
	if req.FormatMode == "json" {
		formats = []string{"json", "a/n", "binary"}
	}
	var appName any
	if req.AppName != "" {
		appName = req.AppName
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"responseId":   randHex(8),
		"responseTime": time.Now().UTC().Format(time.RFC3339Nano),
		"status":       "ok",
		"ticket":       ticket,
		"websocket": map[string]any{
			"id":         id,
			"url":        wsURL,
			"protocol":   []string{"dmdata.v2"},
			"expiration": 300,
		},
		"classifications": req.Classifications,
		"test":            req.Test,
		"types":           req.Types,
		"formats":         formats,
		"appName":         appName,
	})
}

// Reserve occupies a connection slot as if a previous process with appName
// had connected and then died without Socket Close, and returns its id.
func (s *Server) Reserve(appName string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	s.sockets[id] = &socket{
		id:     id,
		req:    StartRequest{Classifications: []string{"eew.forecast"}, Test: "no", AppName: appName, FormatMode: "raw"},
		opened: true,
		start:  time.Now(),
	}
	return id
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeAPIError(w, http.StatusUnauthorized, "Unauthorized.")
		return
	}
	q := r.URL.Query()
	limit := 20
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	if s.listPageSize > 0 && s.listPageSize < limit {
		limit = s.listPageSize
	}
	offset := 0
	if c := q.Get("cursorToken"); c != "" {
		v, err := strconv.Atoi(strings.TrimPrefix(c, "cur"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "Invalid cursorToken.")
			return
		}
		offset = v
	}
	want := q.Get("status")

	s.mu.Lock()
	ids := make([]int, 0, len(s.sockets))
	for id := range s.sockets {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var items []map[string]any
	for _, id := range ids {
		sk := s.sockets[id]
		status := "waiting"
		if sk.opened {
			status = "open"
		}
		if want != "" && want != status {
			continue
		}
		var appName any
		if sk.req.AppName != "" {
			appName = sk.req.AppName
		}
		items = append(items, map[string]any{
			"id":              id,
			"ticket":          nil,
			"classifications": sk.req.Classifications,
			"types":           sk.req.Types,
			"test":            sk.req.Test,
			"formats":         []string{"xml", "a/n", "binary"},
			"appName":         appName,
			"status":          status,
			"start":           sk.start.UTC().Format(time.RFC3339Nano),
			"end":             nil,
			"ping":            nil,
			"ipAddress":       "127.0.0.1",
			"server":          "dmdatasim",
		})
	}
	s.mu.Unlock()

	resp := map[string]any{
		"responseId":   randHex(8),
		"responseTime": time.Now().UTC().Format(time.RFC3339Nano),
		"status":       "ok",
	}
	offset = min(offset, len(items))
	end := min(offset+limit, len(items))
	page := items[offset:end]
	if page == nil {
		page = []map[string]any{}
	}
	resp["items"] = page
	if end < len(items) {
		resp["nextToken"] = "cur" + strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleClose(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeAPIError(w, http.StatusUnauthorized, "Unauthorized.")
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	s.mu.Lock()
	sk, ok := s.sockets[id]
	if err != nil || !ok {
		s.mu.Unlock()
		writeAPIError(w, http.StatusNotFound, "Socket ID not found.")
		return
	}
	delete(s.sockets, id)
	for t, tid := range s.tickets {
		if tid == id {
			delete(s.tickets, t)
		}
	}
	s.closed = append(s.closed, id)
	s.mu.Unlock()
	if sk.cancel != nil {
		sk.cancel()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("ticket")
	s.mu.Lock()
	id, ok := s.tickets[ticket]
	delete(s.tickets, ticket) // single-use, even if the upgrade fails
	var sk *socket
	if ok {
		sk = s.sockets[id]
	}
	s.mu.Unlock()

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"dmdata.v2"}})
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if sk == nil {
		_ = writeMsg(ctx, conn, nil, map[string]any{"type": "error", "error": "No valid ticket.", "code": 4404, "close": true})
		conn.Close(4404, "No valid ticket.")
		return
	}
	if conn.Subprotocol() != "dmdata.v2" {
		conn.Close(websocket.StatusPolicyViolation, "subprotocol required")
		s.dropSocket(id)
		return
	}

	s.mu.Lock()
	sk.conn = conn
	sk.cancel = cancel
	sk.opened = true
	s.mu.Unlock()
	// A clean close (close frame) or Socket Close frees the slot at once.
	// An unclean disconnect leaves it held, like the real service does
	// until its ping timeout, so only Socket Close frees it.
	unclean := false
	defer func() {
		if unclean && ctx.Err() == nil {
			s.mu.Lock()
			sk.conn = nil
			s.mu.Unlock()
			return
		}
		s.dropSocket(id)
	}()

	var appName any
	if sk.req.AppName != "" {
		appName = sk.req.AppName
	}
	if err := writeMsg(ctx, conn, &sk.writeMu, map[string]any{
		"type":            "start",
		"socketId":        id,
		"classifications": sk.req.Classifications,
		"types":           sk.req.Types,
		"test":            sk.req.Test,
		"formats":         []string{"xml", "a/n", "binary"},
		"appName":         appName,
		"time":            time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return
	}
	s.connected <- id

	if s.pingInterval > 0 {
		go func() {
			t := time.NewTicker(s.pingInterval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					_ = s.Ping(id)
				}
			}
		}()
	}

	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			unclean = websocket.CloseStatus(err) == -1
			conn.CloseNow()
			return
		}
		var msg struct {
			Type   string `json:"type"`
			PingID string `json:"pingId"`
		}
		if json.Unmarshal(raw, &msg) != nil {
			_ = writeMsg(ctx, conn, &sk.writeMu, map[string]any{"type": "error", "error": "Invalid data.", "code": 4641, "close": true})
			conn.Close(4641, "Invalid data.")
			return
		}
		switch msg.Type {
		case "pong":
			s.mu.Lock()
			okPong := msg.PingID == sk.lastPID
			if okPong {
				sk.ponged = true
				s.pongs = append(s.pongs, msg.PingID)
			}
			s.mu.Unlock()
			if !okPong {
				_ = writeMsg(ctx, conn, &sk.writeMu, map[string]any{"type": "error", "error": "Ping ID mismatch.", "code": 4640, "close": true})
				conn.Close(4640, "Ping ID mismatch.")
				return
			}
		case "ping":
			_ = writeMsg(ctx, conn, &sk.writeMu, map[string]any{"type": "pong", "pingId": msg.PingID})
		}
	}
}

func (s *Server) dropSocket(id int) {
	s.mu.Lock()
	delete(s.sockets, id)
	s.mu.Unlock()
}

func writeMsg(ctx context.Context, conn *websocket.Conn, mu *sync.Mutex, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeRaw(ctx, conn, mu, b)
}

func writeRaw(ctx context.Context, conn *websocket.Conn, mu *sync.Mutex, b []byte) error {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

func (s *Server) get(id int) (*socket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.sockets[id]
	if !ok || sk.conn == nil {
		return nil, fmt.Errorf("dmdatasim: socket %d not connected", id)
	}
	return sk, nil
}

// Ping sends a ping with a fresh pingId to socket id.
func (s *Server) Ping(id int) error {
	sk, err := s.get(id)
	if err != nil {
		return err
	}
	pid := randHex(6)
	s.mu.Lock()
	sk.lastPID = pid
	sk.ponged = false
	s.mu.Unlock()
	return writeMsg(context.Background(), sk.conn, &sk.writeMu, map[string]any{"type": "ping", "pingId": pid})
}

// SendRaw sends an arbitrary text message to socket id.
func (s *Server) SendRaw(id int, msg []byte) error {
	sk, err := s.get(id)
	if err != nil {
		return err
	}
	return writeRaw(context.Background(), sk.conn, &sk.writeMu, msg)
}

// SendError sends an error message; with close it then closes the socket
// with that code, like the real service.
func (s *Server) SendError(id int, code int, text string, close bool) error {
	sk, err := s.get(id)
	if err != nil {
		return err
	}
	if err := writeMsg(context.Background(), sk.conn, &sk.writeMu, map[string]any{"type": "error", "error": text, "code": code, "close": close}); err != nil {
		return err
	}
	if close {
		sk.conn.Close(websocket.StatusCode(code), text)
	}
	return nil
}

// CloseWithCode closes socket id with a close frame carrying code, without
// sending an `error` message first.
func (s *Server) CloseWithCode(id int, code int, reason string) error {
	sk, err := s.get(id)
	if err != nil {
		return err
	}
	return sk.conn.Close(websocket.StatusCode(code), reason)
}

// Drop abruptly closes socket id's connection without a close frame or
// error message (like a network failure or a server restart).
func (s *Server) Drop(id int) error {
	sk, err := s.get(id)
	if err != nil {
		return err
	}
	sk.conn.CloseNow()
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
