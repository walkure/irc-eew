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

const (
	initialBackoff = 1 * time.Second
	maxBackoff     = 60 * time.Second
)

// Connection manages one IRC server: connecting, joining its configured
// channels, reconnecting with backoff on disconnect, and draining its send
// Queue once registered. Unlike irc-eew.pl's single IO::Select loop shared
// across IRC/WNI/Slack, each Connection owns its own goroutines and never
// blocks the WNI receive path — see Notify.
type Connection struct {
	server     string
	conn       *goirc.Conn
	dispatcher *Dispatcher
	queue      *Queue
	charset    string
	reconnect  chan struct{}

	mu    sync.Mutex
	ready chan struct{} // closed once JOINed; replaced on every disconnect
}

// NewConnection builds a Connection for one configured IRC server. It does
// not connect; call Run to start connecting and stay connected until ctx is
// canceled.
func NewConnection(srv config.IRCServerConfig) *Connection {
	installLogger()

	conn := goirc.Client(clientConfig(srv))
	c := &Connection{
		server:     conn.Config().Server,
		conn:       conn,
		dispatcher: NewDispatcher(srv.AllNotice, srv.LimitedNotice),
		queue:      NewQueue(defaultQueueCapacity),
		charset:    srv.Server.Charset,
		reconnect:  make(chan struct{}, 1),
		ready:      make(chan struct{}),
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
// (reset on every successful connect) until ctx is canceled, at which point
// it sends QUIT, closes the connection, and returns.
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
		backoff = initialBackoff

		select {
		case <-c.reconnect:
			continue
		case <-ctx.Done():
		}
		break
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
		for _, ch := range n.Channels {
			channel, err := EncodeText(c.charset, ch)
			if err != nil {
				slog.Error("irc: encoding channel name", "server", c.server, "channel", ch, "error", err)
				continue
			}
			c.conn.Notice(channel, text)
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
