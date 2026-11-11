package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/walkure/irc-eew/internal/dmdata"
	"github.com/walkure/irc-eew/internal/dmdatasim"
	"github.com/walkure/irc-eew/internal/rawarchive"
)

// TestFeedLoop_ArchivesAndReconnects drives runFeedLoop against the fake
// DMDATA server: a delivered message must be archived verbatim, and after
// the server drops the session the loop must come back on a new ticket.
func TestFeedLoop_ArchivesAndReconnects(t *testing.T) {
	const key = "AKe.test"
	srv := dmdatasim.New(key)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	archive, err := rawarchive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	client := &dmdata.Client{
		API:     dmdata.NewAPI(srv.URL(), key),
		Request: dmdata.SocketStartRequest{Classifications: []string{"eew.forecast"}, Types: []string{"VXSE45"}},
	}
	received := make(chan struct{}, 4)
	h := dmdata.Handlers{OnData: func(m *dmdata.DataMessage) {
		handleData(archive, m)
		received <- struct{}{}
	}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); runFeedLoop(ctx, client, h) }()
	t.Cleanup(func() { cancel(); <-done })

	waitID := func() int {
		select {
		case id := <-srv.Connected():
			return id
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for a connection")
			return 0
		}
	}

	id := waitID()
	raw := dmdatasim.DataMessage(dmdatasim.Telegram{Body: []byte("<Report/>")})
	if err := srv.SendRaw(id, raw); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("data not received")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*", "*", "*", "*.json"))
	if len(files) != 1 {
		t.Fatalf("archived files = %v, want 1", files)
	}
	if got, _ := os.ReadFile(files[0]); !bytes.Equal(got, raw) {
		t.Error("archived content is not the message verbatim")
	}

	if err := srv.SendError(id, 4503, "Server error.", true); err != nil {
		t.Fatal(err)
	}
	if id2 := waitID(); id2 == id {
		t.Errorf("reconnected with the same socket id %d", id2)
	}
	if n := len(srv.StartRequests()); n != 2 {
		t.Errorf("Socket Start calls = %d, want 2 (one ticket per session)", n)
	}
}
