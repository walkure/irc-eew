package dmdata

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func strp(s string) *string { return &s }

func TestDecodeBody_PlainUTF8(t *testing.T) {
	m := &DataMessage{Encoding: strp("utf-8"), Body: "37 03 00 本文"}
	got, err := m.DecodeBody()
	if err != nil || string(got) != "37 03 00 本文" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDecodeBody_NullEncodingAndCompression(t *testing.T) {
	m := &DataMessage{Body: "abc"}
	got, err := m.DecodeBody()
	if err != nil || string(got) != "abc" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDecodeBody_Base64Uncompressed(t *testing.T) {
	m := &DataMessage{Encoding: strp("base64"), Body: base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0xff})}
	got, err := m.DecodeBody()
	if err != nil || !bytes.Equal(got, []byte{0x01, 0x02, 0xff}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestDecodeBody_Zip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("telegram.xml")
	_, _ = w.Write([]byte("<x/>"))
	_ = zw.Close()
	m := &DataMessage{Encoding: strp("base64"), Compression: strp("zip"), Body: base64.StdEncoding.EncodeToString(buf.Bytes())}
	got, err := m.DecodeBody()
	if err != nil || string(got) != "<x/>" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDecodeBody_UnknownEncoding(t *testing.T) {
	m := &DataMessage{Encoding: strp("rot13"), Body: "x"}
	if _, err := m.DecodeBody(); err == nil {
		t.Fatal("expected error")
	}
}

func TestSocketID_AcceptsIntAndString(t *testing.T) {
	var v struct {
		A SocketID `json:"a"`
		B SocketID `json:"b"`
	}
	if err := json.Unmarshal([]byte(`{"a":123,"b":"456"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.A != "123" || v.B != "456" {
		t.Fatalf("got %+v", v)
	}
}
