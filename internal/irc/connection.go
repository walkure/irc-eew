package irc

import (
	"context"
	"log/slog"
	"sync"
	"time"

	goirc "github.com/fluffle/goirc/client"

	"github.com/walkure/irc-eew/internal/config"
	"github.com/walkure/irc-eew/internal/decoder"
)

// defaultQueueCapacity bounds how many distinct earthquakes' notices one
// Connection will hold while disconnected or falling behind. It has no
// analogue in irc-eew.pl, which had no backlog concept at all.
const defaultQueueCapacity = 32

// messageType selects which IRC command a Connection uses to notify a
// channel.
type messageType int

const (
	messageTypeNotice messageType = iota
	messageTypePrivmsg
)

// parseMessageType resolves a config.IRCServerConfig.Server.MessageType
// string. ok is false for an unrecognized non-empty value, in which case
// the returned messageType is still the safe default (notice).
func parseMessageType(s string) (mt messageType, ok bool) {
	switch s {
	case "", "notice":
		return messageTypeNotice, true
	case "privmsg":
		return messageTypePrivmsg, true
	default:
		return messageTypeNotice, false
	}
}

const (
	initialBackoff = 1 * time.Second
	maxBackoff     = 60 * time.Second

	// stableConnectionThreshold is how long a connection must stay up
	// before Run treats it as a real recovery and resets backoff to
	// initialBackoff. Without this, a server that accepts a connection
	// and then kills it almost immediately (observed against a real
	// network: repeated connect/disconnect roughly once a second) would
	// make every attempt look "successful" and defeat backoff entirely,
	// hammering the server for as long as that behavior continues.
	stableConnectionThreshold = 30 * time.Second
)

// Connection manages one IRC server: connecting, joining its configured
// channels, reconnecting with backoff on disconnect, and draining its send
// Queue once registered. Unlike irc-eew.pl's single IO::Select loop shared
// across IRC/WNI/Slack, each Connection owns its own goroutines and never
// blocks the WNI receive path — see Notify.
type Connection struct {
	server      string
	conn        *goirc.Conn
	dispatcher  *Dispatcher
	queue       *Queue
	charset     string
	messageType messageType
	reconnect   chan struct{}

	mu    sync.Mutex
	ready chan struct{} // closed once JOINed; replaced on every disconnect
}

// NewConnection builds a Connection for one configured IRC server. It does
// not connect; call Run to start connecting and stay connected until ctx is
// canceled.
func NewConnection(srv config.IRCServerConfig) *Connection {
	installLogger()

	mt, ok := parseMessageType(srv.Server.MessageType)
	if !ok {
		slog.Error("irc: unknown message-type, defaulting to notice", "server", srv.Server.Host, "message-type", srv.Server.MessageType)
	}

	conn := goirc.Client(clientConfig(srv))
	c := &Connection{
		server:      conn.Config().Server,
		conn:        conn,
		dispatcher:  NewDispatcher(srv.AllNotice, srv.LimitedNotice),
		queue:       NewQueue(defaultQueueCapacity),
		charset:     srv.Server.Charset,
		messageType: mt,
		reconnect:   make(chan struct{}, 1),
		ready:       make(chan struct{}),
	}
	conn.HandleFunc(goirc.CONNECTED, c.handleConnected)
	conn.HandleFunc(goirc.DISCONNECTED, c.handleDisconnected)
	return c
}

// Notify dispatches a decoded telegram to this connection's channels
// (per its all-notice/limited-notice configuration) and enqueues it for
// sending. It never blocks — Queue.Enqueue is non-blocking by design — so
// it's safe to call directly from the WNI receive path.
func (c *Connection) Notify(t *decoder.Telegram, slackText string) {
	channels, priority := c.dispatcher.ChannelsFor(t)
	if len(channels) == 0 {
		return
	}
	c.queue.Enqueue(Notice{
		EqID:     t.EqID,
		Channels: channels,
		Text:     StripLinks(slackText),
		Priority: priority,
	})
}

// Run connects to the server, reconnecting with capped exponential backoff
// until ctx is canceled, at which point it sends QUIT, closes the
// connection, and returns. Backoff only resets once a connection has stayed
// up for at least stableConnectionThreshold — a connection that dies sooner
// than that keeps growing the delay instead, same as a failed connect.
func (c *Connection) Run(ctx context.Context) {
	go c.writeLoop(ctx)

	backoff := initialBackoff
	for ctx.Err() == nil {
		if err := c.conn.ConnectContext(ctx); err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Warn("irc connect failed, retrying", "server", c.server, "error", err, "backoff", backoff)
			if !sleepCtx(ctx, backoff) {
				break
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}
		connectedAt := time.Now()

		select {
		case <-c.reconnect:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}

		connectedFor := time.Since(connectedAt)
		backoff = backoffAfterDisconnect(backoff, connectedFor)
		if connectedFor >= stableConnectionThreshold {
			continue
		}

		slog.Warn("irc connection was short-lived, backing off before retrying", "server", c.server, "uptime", connectedFor, "backoff", backoff)
		if !sleepCtx(ctx, backoff) {
			break
		}
	}

	if c.conn.Connected() {
		c.conn.Quit()
		c.conn.Close()
	}
	c.queue.Close()
}

func (c *Connection) handleConnected(conn *goirc.Conn, _ *goirc.Line) {
	for _, ch := range c.dispatcher.JoinChannels() {
		encoded, err := EncodeText(c.charset, ch)
		if err != nil {
			slog.Error("irc: encoding channel name for JOIN", "server", c.server, "channel", ch, "error", err)
			continue
		}
		conn.Join(encoded)
	}
	slog.Info("irc connected", "server", c.server)

	c.mu.Lock()
	close(c.ready)
	c.mu.Unlock()
}

func (c *Connection) handleDisconnected(_ *goirc.Conn, _ *goirc.Line) {
	slog.Warn("irc disconnected", "server", c.server)

	c.mu.Lock()
	c.ready = make(chan struct{})
	c.mu.Unlock()

	select {
	case c.reconnect <- struct{}{}:
	default:
	}
}

// waitReady blocks until the connection has JOINed its channels (or ctx is
// canceled, returning false).
func (c *Connection) waitReady(ctx context.Context) bool {
	c.mu.Lock()
	ch := c.ready
	c.mu.Unlock()

	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}

// writeLoop drains the send queue once the connection is ready. It waits
// for readiness before dequeuing (not after), so a Notice stays eligible
// for coalescing (see Queue) for as long as possible while disconnected.
func (c *Connection) writeLoop(ctx context.Context) {
	for {
		if !c.waitReady(ctx) {
			return
		}

		n, ok := c.queue.Dequeue()
		if !ok {
			return
		}

		text, err := EncodeText(c.charset, n.Text)
		if err != nil {
			slog.Error("irc: encoding notice text", "server", c.server, "error", err)
			continue
		}
		send := c.conn.Notice
		if c.messageType == messageTypePrivmsg {
			send = c.conn.Privmsg
		}
		for _, ch := range n.Channels {
			channel, err := EncodeText(c.charset, ch)
			if err != nil {
				slog.Error("irc: encoding channel name", "server", c.server, "channel", ch, "error", err)
				continue
			}
			send(channel, text)
		}
		slog.Info("irc notified", "server", c.server, "channels", n.Channels, "eq_id", n.EqID)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}

// backoffAfterDisconnect returns the backoff to use for the next connect
// attempt given how long the just-ended connection stayed up. A connection
// that was stable for at least stableConnectionThreshold resets to
// initialBackoff; anything shorter grows cur the same way a failed connect
// would, so a server that keeps accepting and then instantly dropping us
// can't defeat backoff by making every attempt look "successful".
func backoffAfterDisconnect(cur, connectedFor time.Duration) time.Duration {
	if connectedFor >= stableConnectionThreshold {
		return initialBackoff
	}
	return nextBackoff(cur, maxBackoff)
}
