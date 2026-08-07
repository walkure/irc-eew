// Command wnisim runs a fake WNI FastCaster server for manually verifying
// protocol compatibility. It is a development/verification tool, not part
// of the production notifier.
//
// Typical use: point a patched copy of the (10-years-in-production) Perl
// irc-eew.pl at this server's address instead of the real WNI server list,
// feed it a directory of real captured telegrams (e.g. from eewlog/), and
// confirm the unmodified Perl decode/notify pipeline behaves as expected —
// this validates that wnisim's protocol implementation is faithful to the
// real WNI server before the Go client (internal/wni) is tested against it.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/walkure/irc-eew/internal/wnisim"
)

func main() {
	listen := flag.String("listen", ":19000", "address to listen on")
	dir := flag.String("telegrams-dir", "", "directory of raw telegram files to replay after login (sorted by filename)")
	interval := flag.Duration("interval", 3*time.Second, "delay between telegrams")
	keepAlive := flag.Duration("keepalive-interval", 30*time.Second, "interval between Keep-Alive blocks (0 disables)")
	getPingEvery := flag.Int("get-ping-every", 1, "send the GET / HTTP/1.1 ack quirk before every Nth telegram (0 disables)")
	once := flag.Bool("once", false, "exit after handling a single connection")
	flag.Parse()

	if *dir == "" {
		log.Fatal("-telegrams-dir is required")
	}

	files, err := telegramFiles(*dir)
	if err != nil {
		log.Fatalf("listing telegrams: %v", err)
	}
	if len(files) == 0 {
		log.Fatalf("no files found in %s", *dir)
	}
	log.Printf("loaded %d telegram file(s) from %s", len(files), *dir)

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("listen %s: %v", *listen, err)
	}
	log.Printf("wnisim listening on %s", ln.Addr())

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatalf("accept: %v", err)
		}
		log.Printf("connection from %s", conn.RemoteAddr())
		handleConn(conn, files, *interval, *keepAlive, *getPingEvery)
		if *once {
			return
		}
	}
}

func telegramFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func handleConn(conn net.Conn, files []string, interval, keepAlive time.Duration, getPingEvery int) {
	defer conn.Close()
	sess := wnisim.Accept(conn)

	headers, err := sess.AwaitLogin(10 * time.Second)
	if err != nil {
		log.Printf("login failed: %v", err)
		return
	}
	log.Printf("login received: account=%q id=%q protocol=%q",
		headers["X-WNI-Account"], headers["X-WNI-ID"], headers["X-WNI-Protocol-Version"])

	if err := sess.SendResponseOK(); err != nil {
		log.Printf("send response: %v", err)
		return
	}
	log.Printf("sent login Response OK")

	stopKeepAlive := make(chan struct{})
	if keepAlive > 0 {
		go func() {
			t := time.NewTicker(keepAlive)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					if err := sess.SendKeepAlive(); err != nil {
						return
					}
					log.Printf("sent Keep-Alive")
				case <-stopKeepAlive:
					return
				}
			}
		}()
		defer close(stopKeepAlive)
	}

	for i, path := range files {
		if getPingEvery > 0 && i%getPingEvery == 0 {
			if err := sess.SendGETPing(); err != nil {
				log.Printf("send GET ping: %v", err)
				return
			}
			log.Printf("sent GET / HTTP/1.1 ping, waiting for ack...")
			if sess.AwaitAck(5 * time.Second) {
				log.Printf("client acked the GET ping")
			} else {
				log.Printf("WARNING: no ack received for GET ping within timeout")
			}
			time.Sleep(200 * time.Millisecond)
		}

		body, err := os.ReadFile(path)
		if err != nil {
			log.Printf("read %s: %v", path, err)
			continue
		}
		if len(body) == 0 {
			log.Printf("skipping empty file %s", path)
			continue
		}
		if err := sess.SendData(body); err != nil {
			log.Printf("send data: %v", err)
			return
		}
		log.Printf("sent Data block %d/%d (%s, %d bytes)", i+1, len(files), filepath.Base(path), len(body))

		time.Sleep(interval)
	}

	log.Printf("all telegrams sent; idling (Ctrl+C to stop, or disconnect to test client reconnect)")
	select {}
}
