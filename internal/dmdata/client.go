package dmdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/coder/websocket"
)

// Protocol is the WebSocket subprotocol DMDATA.JP's WebSocket v2 speaks.
const Protocol = "dmdata.v2"

// DefaultIdleTimeout is how long RunSession waits for any server message
// before giving up on the connection. The server pings periodically and
// drops clients that haven't answered within 120s, so a silent connection
// well past that is assumed dead.
const DefaultIdleTimeout = 180 * time.Second

// readLimit bounds one WebSocket message. EEW telegrams are a few KB even
// before compression; this is generous headroom for other telegram types.
const readLimit = 8 << 20

// closeAPITimeout bounds the best-effort Socket Close call made when a
// session ends.
const closeAPITimeout = 10 * time.Second

// Handlers receives session events. All callbacks run on the session's
// read goroutine, so they must not block for long (a slow handler delays
// the pong reply the server requires).
type Handlers struct {
	// OnStart is called when the server confirms the connection.
	OnStart func(StartMessage)
	// OnData is called for each delivered telegram.
	OnData func(*DataMessage)
	// OnPing, if set, is called after each server ping has been answered.
	OnPing func(pingID string)
}

// Client opens WebSocket v2 sessions. It is not itself a reconnect loop:
// each RunSession is one ticket, one connection, until that connection
// ends — the caller owns retry/backoff policy.
type Client struct {
	API     *API
	Request SocketStartRequest
	// IdleTimeout overrides DefaultIdleTimeout when > 0.
	IdleTimeout time.Duration
}

// ServerError is an `error` message the server sent with close:true.
type ServerError struct {
	ErrorMessage
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("dmdata websocket: server error %d: %s", e.Code, e.ErrorMessage.Error)
}

// RunSession issues a ticket, connects, and reads until the connection
// ends or ctx is canceled. established is the zero time if the WebSocket
// was never opened. On return the socket's slot is released via Socket
// Close (best effort), so a dead connection doesn't keep holding it until
// the server's ping timeout.
//
// If Socket Start is refused because every connection slot is taken (409),
// RunSession closes this app's stale sockets (see CloseStale) and tries
// Socket Start once more.
func (c *Client) RunSession(ctx context.Context, h Handlers) (established time.Time, err error) {
	start, err := c.API.StartSocket(ctx, c.Request)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus == 409 {
		closed, cerr := c.CloseStale(ctx)
		if cerr != nil {
			slog.Warn("dmdata stale socket cleanup failed", "error", cerr)
		}
		if closed > 0 {
			start, err = c.API.StartSocket(ctx, c.Request)
		}
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("socket start: %w", err)
	}
	socketID := start.WebSocket.ID
	defer c.closeSocket(socketID)

	protocols := start.WebSocket.Protocol
	if len(protocols) == 0 {
		protocols = []string{Protocol}
	}
	conn, _, err := websocket.Dial(ctx, start.WebSocket.URL, &websocket.DialOptions{
		Subprotocols: protocols,
		HTTPClient:   c.API.HTTP,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("websocket dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(readLimit)
	established = time.Now()

	err = c.readLoop(ctx, conn, h)
	if ctx.Err() != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return established, nil
	}
	return established, err
}

// CloseStale closes every "open" or "waiting" socket whose appName equals
// c.Request.AppName — left behind when a previous run of this app died
// without calling Socket Close, and otherwise holding a connection slot
// until the server's ping timeout (open) or ticket expiry (waiting).
//
// It must only be called while this Client has no session of its own
// running. It does nothing when AppName is empty, since that would match
// other applications' sockets on the same account. Running two instances
// (e.g. shadow and production) with the same AppName on one account would
// make them close each other's sockets: give each its own AppName.
func (c *Client) CloseStale(ctx context.Context) (closed int, err error) {
	app := c.Request.AppName
	if app == "" {
		return 0, nil
	}
	// Query each live state separately: an unfiltered list would page
	// through the account's whole closed-socket history.
	var items []SocketInfo
	for _, status := range []string{"open", "waiting"} {
		got, err := c.API.ListSockets(ctx, status)
		if err != nil {
			return 0, fmt.Errorf("socket list (%s): %w", status, err)
		}
		items = append(items, got...)
	}
	var errs []error
	for _, it := range items {
		if it.AppName == nil || *it.AppName != app {
			continue
		}
		if it.Status != "open" && it.Status != "waiting" {
			continue
		}
		cerr := c.API.CloseSocket(ctx, it.ID)
		var apiErr *APIError
		switch {
		case cerr == nil:
			closed++
			slog.Info("dmdata stale socket closed", "socket_id", it.ID, "status", it.Status, "start", it.Start)
		case errors.As(cerr, &apiErr) && apiErr.HTTPStatus == 404:
			// Went away on its own in the meantime.
		default:
			errs = append(errs, fmt.Errorf("closing socket %s: %w", it.ID, cerr))
		}
	}
	return closed, errors.Join(errs...)
}

func (c *Client) closeSocket(id SocketID) {
	ctx, cancel := context.WithTimeout(context.Background(), closeAPITimeout)
	defer cancel()
	err := c.API.CloseSocket(ctx, id)
	var apiErr *APIError
	switch {
	case err == nil:
		slog.Debug("dmdata socket closed", "socket_id", id)
	case errors.As(err, &apiErr) && apiErr.HTTPStatus == 404:
		// Already gone server-side (the usual case after a server close).
		slog.Debug("dmdata socket already closed", "socket_id", id)
	default:
		slog.Warn("dmdata socket close failed", "socket_id", id, "error", err)
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, h Handlers) error {
	idle := c.IdleTimeout
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	for {
		readCtx, cancel := context.WithTimeout(ctx, idle)
		_, raw, err := conn.Read(readCtx)
		idleExpired := readCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil
		cancel()
		if err != nil {
			if idleExpired {
				return fmt.Errorf("websocket: no message for %s", idle)
			}
			return fmt.Errorf("websocket read: %w", err)
		}
		if err := c.handle(ctx, conn, raw, h); err != nil {
			return err
		}
	}
}

func (c *Client) handle(ctx context.Context, conn *websocket.Conn, raw []byte, h Handlers) error {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		slog.Warn("dmdata: undecodable message", "error", err, "len", len(raw))
		return nil
	}
	switch env.Type {
	case "ping":
		var p pingMessage
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("dmdata: decoding ping: %w", err)
		}
		pong, _ := json.Marshal(pongMessage{Type: "pong", PingID: p.PingID})
		writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := conn.Write(writeCtx, websocket.MessageText, pong); err != nil {
			return fmt.Errorf("websocket pong: %w", err)
		}
		if h.OnPing != nil {
			h.OnPing(p.PingID)
		}
	case "pong":
		// Reply to a client ping; this client never sends any.
	case "start":
		var s StartMessage
		if err := json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("dmdata: decoding start: %w", err)
		}
		if h.OnStart != nil {
			h.OnStart(s)
		}
	case "data":
		m, err := parseData(raw)
		if err != nil {
			// Still hand the raw bytes over so they can be archived.
			slog.Warn("dmdata: malformed data message", "error", err)
			m = &DataMessage{Raw: raw}
		}
		if h.OnData != nil {
			h.OnData(m)
		}
	case "error":
		var e ErrorMessage
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("dmdata: decoding error message: %w", err)
		}
		if e.Close {
			return &ServerError{e}
		}
		slog.Warn("dmdata server error", "code", e.Code, "error", e.Error)
	default:
		slog.Warn("dmdata: unknown message type", "type", env.Type)
	}
	return nil
}
