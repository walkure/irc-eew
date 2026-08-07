package wni_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/walkure/irc-eew/internal/wni"
	"github.com/walkure/irc-eew/internal/wnisim"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

// TestClient_FullHandshake drives a real wnisim server (the same
// implementation manually cross-checked against the ~10-years-in-production
// Perl EEWSock.pm) through login, the GET-ping ack quirk, a Data block, and
// a Keep-Alive, and asserts the Go client observes all of it correctly.
func TestClient_FullHandshake(t *testing.T) {
	ln := listen(t)

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		sess := wnisim.Accept(conn)

		headers, err := sess.AwaitLogin(5 * time.Second)
		if err != nil {
			serverErr <- err
			return
		}
		if headers["X-WNI-Account"] != "testuser" {
			serverErr <- fmt.Errorf("unexpected account %q", headers["X-WNI-Account"])
			return
		}
		if err := sess.SendResponseOK(); err != nil {
			serverErr <- err
			return
		}
		if err := sess.SendGETPing(); err != nil {
			serverErr <- err
			return
		}
		if !sess.AwaitAck(3 * time.Second) {
			serverErr <- fmt.Errorf("client did not ack the GET ping")
			return
		}
		if err := sess.SendData([]byte("hello world telegram")); err != nil {
			serverErr <- err
			return
		}
		if err := sess.SendKeepAlive(); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c, err := wni.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if err := c.Login(wni.Credentials{User: "testuser", Passwd: "secret"}); err != nil {
		t.Fatalf("Login: %v", err)
	}

	var gotResponse bool
	var gotData []byte
	var gotKeepAlive bool
	runErr := make(chan error, 1)
	go func() {
		runErr <- c.Run(ctx, 5*time.Second, wni.Handlers{
			OnResponse:  func(map[string]string) { gotResponse = true },
			OnData:      func(md5 string, body []byte) { gotData = body },
			OnKeepAlive: func() { gotKeepAlive = true; cancel() },
		})
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("server: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("server did not finish in time")
	}

	select {
	case err := <-runErr:
		if err != nil && ctx.Err() == nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	if !gotResponse {
		t.Error("expected OnResponse to fire")
	}
	if !bytes.Equal(gotData, []byte("hello world telegram")) {
		t.Errorf("unexpected data: %q", gotData)
	}
	if !gotKeepAlive {
		t.Error("expected OnKeepAlive to fire")
	}
}

func TestClient_RunReturnsErrorOnDisconnect(t *testing.T) {
	ln := listen(t)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		sess := wnisim.Accept(conn)
		if _, err := sess.AwaitLogin(3 * time.Second); err != nil {
			conn.Close()
			return
		}
		sess.SendResponseOK()
		time.Sleep(100 * time.Millisecond)
		conn.Close()
	}()

	ctx := context.Background()
	c, err := wni.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if err := c.Login(wni.Credentials{User: "u", Passwd: "p"}); err != nil {
		t.Fatalf("Login: %v", err)
	}

	err = c.Run(ctx, 5*time.Second, wni.Handlers{})
	if err == nil {
		t.Fatal("expected Run to return an error when the server disconnects")
	}
}

func TestClient_RunReturnsErrorOnIdleTimeout(t *testing.T) {
	ln := listen(t)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sess := wnisim.Accept(conn)
		if _, err := sess.AwaitLogin(3 * time.Second); err != nil {
			return
		}
		sess.SendResponseOK()
		time.Sleep(3 * time.Second) // outlives the client's short idle timeout below, sends nothing more
	}()

	ctx := context.Background()
	c, err := wni.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if err := c.Login(wni.Credentials{User: "u", Passwd: "p"}); err != nil {
		t.Fatalf("Login: %v", err)
	}

	start := time.Now()
	err = c.Run(ctx, 500*time.Millisecond, wni.Handlers{})
	if err == nil {
		t.Fatal("expected Run to return an error on idle timeout")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("idle timeout took too long to trigger: %v", elapsed)
	}
}
