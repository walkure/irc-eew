package wni

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestClient_Run_DispatchesFrameParsedBeforeAckWriteFailure reproduces a
// real production incident: a Data frame that finished parsing in the same
// Feed() batch as a GET-ping quirk line whose ack-write then failed was
// silently dropped, because Run returned the error without dispatching the
// frames Feed had already parsed. See Run's doc comment.
//
// Uses net.Pipe (an in-package test, since it needs the unexported conn/
// parser fields) instead of a real TCP socket: a real socket's ack-write
// only fails once the OS has actually processed the peer's RST, which is a
// timing race a test can't control deterministically. net.Pipe's Write
// blocks until read, and unblocks with an error the instant the peer
// closes, so closing the server side right after the peer has read the
// combined bytes is guaranteed to fail the client's next write.
func TestClient_Run_DispatchesFrameParsedBeforeAckWriteFailure(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	c := &Client{conn: clientSide, parser: NewFrameParser(clientSide)}

	body := []byte("telegram body")
	sum := md5.Sum(body)
	header := fmt.Sprintf("X-WNI-ID: Data\nContent-Length: %d\nX-WNI-Data-MD5: %s\n\n", len(body), hex.EncodeToString(sum[:]))
	combined := append([]byte(header), body...)
	combined = append(combined, []byte("GET / HTTP/1.1\n")...)

	go func() {
		serverSide.Write(combined)
		serverSide.Close()
	}()

	var gotData []byte
	err := c.Run(context.Background(), 2*time.Second, Handlers{
		OnData: func(md5 string, body []byte) { gotData = body },
	})
	if err == nil {
		t.Fatal("expected Run to return an error once the ack write fails")
	}
	if string(gotData) != string(body) {
		t.Errorf("Data frame parsed before the ack-write failure was not dispatched: got %q, want %q", gotData, body)
	}
}
