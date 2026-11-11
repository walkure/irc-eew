package dmdata_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/walkure/irc-eew/internal/dmdata"
	"github.com/walkure/irc-eew/internal/dmdatasim"
)

const testKey = "AKe.test-key"

type session struct {
	done        chan struct{}
	err         error
	established time.Time
	data        chan *dmdata.DataMessage
	starts      chan dmdata.StartMessage
	pings       chan string
	cancel      context.CancelFunc
}

func runSession(t *testing.T, c *dmdata.Client) *session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{
		done:   make(chan struct{}),
		data:   make(chan *dmdata.DataMessage, 8),
		starts: make(chan dmdata.StartMessage, 1),
		pings:  make(chan string, 8),
		cancel: cancel,
	}
	go func() {
		defer close(s.done)
		s.established, s.err = c.RunSession(ctx, dmdata.Handlers{
			OnStart: func(m dmdata.StartMessage) { s.starts <- m },
			OnData:  func(m *dmdata.DataMessage) { s.data <- m },
			OnPing:  func(id string) { s.pings <- id },
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-s.done
	})
	return s
}

func newClient(srv *dmdatasim.Server) *dmdata.Client {
	return &dmdata.Client{
		API: dmdata.NewAPI(srv.URL(), testKey),
		Request: dmdata.SocketStartRequest{
			Classifications: []string{"eew.forecast"},
			Types:           []string{"VXSE45"},
			Test:            "no",
			AppName:         "irc-eew-test",
		},
	}
}

func waitConnected(t *testing.T, srv *dmdatasim.Server) int {
	t.Helper()
	select {
	case id := <-srv.Connected():
		return id
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for websocket connection")
		return 0
	}
}

func waitDone(t *testing.T, s *session) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for session to end")
	}
}

func TestRunSession_StartPingDataAndClose(t *testing.T) {
	srv := dmdatasim.New(testKey)
	t.Cleanup(srv.Close) // registered first so it runs after sessions end

	s := runSession(t, newClient(srv))
	id := waitConnected(t, srv)

	select {
	case m := <-s.starts:
		if string(m.SocketID) == "" {
			t.Errorf("start message without socketId: %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no start message")
	}

	reqs := srv.StartRequests()
	if len(reqs) != 1 || !slices.Equal(reqs[0].Classifications, []string{"eew.forecast"}) ||
		!slices.Equal(reqs[0].Types, []string{"VXSE45"}) || reqs[0].AppName != "irc-eew-test" {
		t.Errorf("unexpected socket start request: %+v", reqs)
	}

	if err := srv.Ping(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.pings:
	case <-time.After(5 * time.Second):
		t.Fatal("ping was not handled")
	}
	// The pong is written before OnPing runs, but the fake records it
	// asynchronously.
	deadline := time.Now().Add(5 * time.Second)
	for len(srv.Pongs()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(srv.Pongs()) != 1 {
		t.Fatalf("pongs = %v, want exactly one matching pong", srv.Pongs())
	}

	body := []byte("<Report>テスト</Report>")
	raw := dmdatasim.DataMessage(dmdatasim.Telegram{Body: body})
	if err := srv.SendRaw(id, raw); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-s.data:
		if !bytes.Equal(m.Raw, raw) {
			t.Error("Raw is not the message exactly as received")
		}
		if m.Head.Type != "VXSE45" || m.Classification != "eew.forecast" || dmdata.StrOrEmpty(m.Format) != "xml" {
			t.Errorf("unexpected parsed fields: %+v", m)
		}
		got, err := m.DecodeBody()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, body) {
			t.Errorf("DecodeBody = %q, want %q", got, body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no data message")
	}

	s.cancel()
	waitDone(t, s)
	if s.err != nil {
		t.Errorf("RunSession after cancel = %v, want nil", s.err)
	}
	if s.established.IsZero() {
		t.Error("established time not reported")
	}
	if n := srv.OpenSockets(); n != 0 {
		t.Errorf("%d connection slots still held after a clean shutdown", n)
	}
}

func TestRunSession_ServerErrorWithClose(t *testing.T) {
	srv := dmdatasim.New(testKey)
	t.Cleanup(srv.Close) // registered first so it runs after sessions end

	s := runSession(t, newClient(srv))
	id := waitConnected(t, srv)
	if err := srv.SendError(id, 4503, "Server error.", true); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	var se *dmdata.ServerError
	if !errors.As(s.err, &se) || se.Code != 4503 {
		t.Fatalf("err = %v, want ServerError 4503", s.err)
	}
}

func TestRunSession_NonClosingServerErrorKeepsSession(t *testing.T) {
	srv := dmdatasim.New(testKey)
	t.Cleanup(srv.Close) // registered first so it runs after sessions end

	s := runSession(t, newClient(srv))
	id := waitConnected(t, srv)
	if err := srv.SendError(id, 4503, "transient", false); err != nil {
		t.Fatal(err)
	}
	if err := srv.SendRaw(id, dmdatasim.DataMessage(dmdatasim.Telegram{Body: []byte("x")})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.data:
	case <-s.done:
		t.Fatalf("session ended on a non-closing error: %v", s.err)
	case <-time.After(5 * time.Second):
		t.Fatal("no data after non-closing error")
	}
}

func TestRunSession_AbruptDropEndsSession(t *testing.T) {
	srv := dmdatasim.New(testKey)
	t.Cleanup(srv.Close) // registered first so it runs after sessions end

	s := runSession(t, newClient(srv))
	id := waitConnected(t, srv)
	if err := srv.Drop(id); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	if s.err == nil {
		t.Fatal("expected an error after an abrupt drop")
	}
	if !slices.Contains(srv.ClosedSockets(), id) || srv.OpenSockets() != 0 {
		t.Errorf("slot not released via Socket Close (closed: %v, open: %d)", srv.ClosedSockets(), srv.OpenSockets())
	}
}

func TestRunSession_IdleTimeout(t *testing.T) {
	srv := dmdatasim.New(testKey)
	t.Cleanup(srv.Close) // registered first so it runs after sessions end

	c := newClient(srv)
	c.IdleTimeout = 200 * time.Millisecond
	s := runSession(t, c)
	id := waitConnected(t, srv)
	waitDone(t, s)
	if s.err == nil {
		t.Fatal("expected an idle-timeout error")
	}
	// The client gave up without a clean close, so only Socket Close can
	// have freed the slot.
	if !slices.Contains(srv.ClosedSockets(), id) || srv.OpenSockets() != 0 {
		t.Errorf("slot not released via Socket Close (closed: %v, open: %d)", srv.ClosedSockets(), srv.OpenSockets())
	}
}

func TestRunSession_BadKey(t *testing.T) {
	srv := dmdatasim.New(testKey)
	t.Cleanup(srv.Close) // registered first so it runs after sessions end

	c := newClient(srv)
	c.API = dmdata.NewAPI(srv.URL(), "wrong")
	established, err := c.RunSession(context.Background(), dmdata.Handlers{})
	var apiErr *dmdata.APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 401 {
		t.Fatalf("err = %v, want APIError 401", err)
	}
	if !established.IsZero() {
		t.Error("established should be zero when never connected")
	}
}

func TestRunSession_ConnectionLimit(t *testing.T) {
	srv := dmdatasim.New(testKey, dmdatasim.WithMaxSockets(1))
	t.Cleanup(srv.Close) // registered first so it runs after sessions end

	// The slot is held by a live session of a different app, which the
	// 409 cleanup must leave alone.
	other := newClient(srv)
	other.Request.AppName = "someone-else"
	runSession(t, other)
	id := waitConnected(t, srv)

	_, err := newClient(srv).RunSession(context.Background(), dmdata.Handlers{})
	var apiErr *dmdata.APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 409 {
		t.Fatalf("err = %v, want APIError 409", err)
	}
	if slices.Contains(srv.ClosedSockets(), id) {
		t.Error("closed another app's socket")
	}
}

// A previous run that died without Socket Close left this app's slot
// held; on 409 the client must close it and get a session.
func TestRunSession_409ClosesOwnStaleSocketAndRetries(t *testing.T) {
	srv := dmdatasim.New(testKey, dmdatasim.WithMaxSockets(1))
	t.Cleanup(srv.Close)

	stale := srv.Reserve("irc-eew-test")
	s := runSession(t, newClient(srv))
	waitConnected(t, srv)
	if !slices.Contains(srv.ClosedSockets(), stale) {
		t.Errorf("stale socket %d not closed (closed: %v)", stale, srv.ClosedSockets())
	}
	select {
	case <-s.done:
		t.Fatalf("session ended: %v", s.err)
	default:
	}
}

func TestCloseStale_OnlyOwnAppAndFollowsPagination(t *testing.T) {
	srv := dmdatasim.New(testKey, dmdatasim.WithMaxSockets(10), dmdatasim.WithListPageSize(2))
	t.Cleanup(srv.Close)

	var mine []int
	for range 3 {
		mine = append(mine, srv.Reserve("irc-eew-test"))
	}
	theirs := srv.Reserve("other-app")
	noName := srv.Reserve("")

	closed, err := newClient(srv).CloseStale(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := srv.ClosedSockets()
	slices.Sort(got)
	if closed != 3 || !slices.Equal(got, mine) {
		t.Errorf("closed %d sockets %v, want %v", closed, got, mine)
	}
	if srv.OpenSockets() != 2 {
		t.Errorf("open sockets = %d, want 2 (ids %d, %d kept)", srv.OpenSockets(), theirs, noName)
	}
}

func TestCloseStale_EmptyAppNameDoesNothing(t *testing.T) {
	srv := dmdatasim.New(testKey)
	t.Cleanup(srv.Close)
	srv.Reserve("")

	c := newClient(srv)
	c.Request.AppName = ""
	closed, err := c.CloseStale(context.Background())
	if err != nil || closed != 0 || len(srv.ClosedSockets()) != 0 {
		t.Fatalf("closed %d, err %v, server closed %v", closed, err, srv.ClosedSockets())
	}
}
