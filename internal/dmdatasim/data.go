package dmdatasim

import (
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Telegram describes one telegram for DataMessage to wrap.
type Telegram struct {
	Classification string // default "eew.forecast"
	Type           string // head.type, default "VXSE45"
	Format         string // "xml" (default), "json", "a/n"
	Test           bool
	Body           []byte
}

// DataMessage builds a WebSocket v2 `data` message the way the real
// service sends XML/JSON telegrams: gzip-compressed, base64-encoded.
func DataMessage(t Telegram) []byte {
	if t.Classification == "" {
		t.Classification = "eew.forecast"
	}
	if t.Type == "" {
		t.Type = "VXSE45"
	}
	if t.Format == "" {
		t.Format = "xml"
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(t.Body)
	_ = zw.Close()

	sum := sha512.Sum384(t.Body)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	isXML := t.Format == "xml"
	msg := map[string]any{
		"type":           "data",
		"version":        "2.0",
		"id":             hex.EncodeToString(sum[:]),
		"classification": t.Classification,
		"passing":        []map[string]string{{"name": "dmdatasim", "time": now}},
		"head": map[string]any{
			"type":        t.Type,
			"author":      "JPOS",
			"time":        now,
			"designation": nil,
			"test":        t.Test,
			"xml":         isXML,
		},
		"format":      t.Format,
		"compression": "gzip",
		"encoding":    "base64",
		"body":        base64.StdEncoding.EncodeToString(buf.Bytes()),
	}
	b, _ := json.Marshal(msg)
	return b
}
