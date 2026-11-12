package dmdata

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// maxBodySize bounds a decompressed telegram body, guarding against a
// malformed or hostile compressed payload. Real EEW telegrams are a few KB.
const maxBodySize = 16 << 20

// envelope is just enough of any server message to dispatch on its type.
type envelope struct {
	Type string `json:"type"`
}

// StartMessage is sent by the server once the connection is established.
type StartMessage struct {
	SocketID        SocketID `json:"socketId"`
	Classifications []string `json:"classifications"`
	Types           []string `json:"types"`
	Test            string   `json:"test"`
	Formats         []string `json:"formats"`
	AppName         *string  `json:"appName"`
	Time            string   `json:"time"`
}

// pingMessage is the server's keep-alive; it must be answered with a pong
// carrying the same pingId or the server disconnects (within 120s).
type pingMessage struct {
	PingID string `json:"pingId"`
}

type pongMessage struct {
	Type   string `json:"type"`
	PingID string `json:"pingId"`
}

// ErrorMessage is a server-reported error. Close reports whether the server
// is closing the connection because of it.
type ErrorMessage struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
	Close bool   `json:"close"`
}

// DataMessage is one delivered telegram. Raw holds the complete message
// exactly as received (the archival unit); the other fields are parsed out
// of it for dispatch and logging.
type DataMessage struct {
	Raw []byte `json:"-"`

	Version        string    `json:"version"`
	ID             string    `json:"id"`
	Classification string    `json:"classification"`
	Passing        []Passing `json:"passing"`
	Head           DataHead  `json:"head"`
	// XMLReport is DMDATA's summary of an XML telegram's Control/Head
	// (present for XML/JSON formats only).
	XMLReport *XMLReport `json:"xmlReport"`
	// Format is "xml", "a/n", "binary", "json", or null.
	Format *string `json:"format"`
	// Compression is "gzip", "zip", or null.
	Compression *string `json:"compression"`
	// Encoding is "base64", "utf-8", or null.
	Encoding *string `json:"encoding"`
	Body     string  `json:"body"`
}

// XMLReport is the part of the envelope's xmlReport this client uses.
type XMLReport struct {
	Head struct {
		EventID FlexString `json:"eventId"`
		Serial  FlexString `json:"serial"`
	} `json:"head"`
}

// FlexString accepts a JSON string or number (DMDATA documents some ids as
// String<Integer> and may switch representations) and keeps its text.
type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	var id SocketID
	if err := id.UnmarshalJSON(b); err != nil {
		if string(b) == "null" {
			*f = ""
			return nil
		}
		return err
	}
	*f = FlexString(id)
	return nil
}

// EventIDSerial returns xmlReport.head's eventId and serial, or empty
// strings when the envelope has none.
func (m *DataMessage) EventIDSerial() (eventID, serial string) {
	if m.XMLReport == nil {
		return "", ""
	}
	return string(m.XMLReport.Head.EventID), string(m.XMLReport.Head.Serial)
}

// Passing is one relay hop the telegram went through.
type Passing struct {
	Name string `json:"name"`
	Time string `json:"time"`
}

// DataHead is the telegram's routing header.
type DataHead struct {
	Type        string  `json:"type"`
	Author      string  `json:"author"`
	Time        string  `json:"time"`
	Designation *string `json:"designation"`
	// Test is only meaningful for XML telegrams; for other formats it's
	// always false and the body itself must be inspected.
	Test bool  `json:"test"`
	XML  *bool `json:"xml"`
}

// StrOrEmpty dereferences an optional string field for logging.
func StrOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// DecodeBody returns the telegram body bytes, undoing the message's
// encoding (base64) and compression (gzip/zip). It never modifies Raw.
func (m *DataMessage) DecodeBody() ([]byte, error) {
	var data []byte
	switch enc := StrOrEmpty(m.Encoding); enc {
	case "base64":
		b, err := base64.StdEncoding.DecodeString(m.Body)
		if err != nil {
			return nil, fmt.Errorf("dmdata: base64 decoding body: %w", err)
		}
		data = b
	case "utf-8", "":
		data = []byte(m.Body)
	default:
		return nil, fmt.Errorf("dmdata: unknown body encoding %q", enc)
	}

	switch comp := StrOrEmpty(m.Compression); comp {
	case "":
		return data, nil
	case "gzip":
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("dmdata: gzip body: %w", err)
		}
		defer zr.Close()
		return readLimited(zr)
	case "zip":
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("dmdata: zip body: %w", err)
		}
		if len(zr.File) != 1 {
			return nil, fmt.Errorf("dmdata: zip body has %d entries, want 1", len(zr.File))
		}
		f, err := zr.File[0].Open()
		if err != nil {
			return nil, fmt.Errorf("dmdata: zip body: %w", err)
		}
		defer f.Close()
		return readLimited(f)
	default:
		return nil, fmt.Errorf("dmdata: unknown body compression %q", comp)
	}
}

func readLimited(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf("dmdata: decompressing body: %w", err)
	}
	if len(b) > maxBodySize {
		return nil, fmt.Errorf("dmdata: decompressed body exceeds %d bytes", maxBodySize)
	}
	return b, nil
}

func parseData(raw []byte) (*DataMessage, error) {
	var m DataMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("dmdata: decoding data message: %w", err)
	}
	m.Raw = raw
	return &m, nil
}
