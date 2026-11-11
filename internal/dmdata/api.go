// Package dmdata is a client for DMDATA.JP's API v2, covering just what the
// EEW notifier needs: Socket Start v2 (issue a WebSocket ticket), Socket
// Close v2 (release a connection slot), and the WebSocket v2 receive loop
// itself (see client.go).
//
// Reference: https://dmdata.jp/docs/reference/api/v2/socket.start/ ,
// https://dmdata.jp/docs/reference/api/v2/socket.close/ ,
// https://dmdata.jp/docs/reference/api/v2/websocket/
package dmdata

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultAPIBase is the production REST API root.
const DefaultAPIBase = "https://api.dmdata.jp"

// apiTimeout bounds one REST round-trip (Socket Start/Close).
const apiTimeout = 30 * time.Second

// API calls DMDATA.JP's REST endpoints with an API key.
type API struct {
	// Base is the API root, e.g. DefaultAPIBase (no trailing slash).
	Base string
	// Key is the DMDATA.JP API key, sent as HTTP Basic auth user with an
	// empty password (the documented, non-deprecated key auth form).
	Key  string
	HTTP *http.Client
}

// NewAPI returns an API for base (DefaultAPIBase if empty) and key.
func NewAPI(base, key string) *API {
	if base == "" {
		base = DefaultAPIBase
	}
	return &API{
		Base: strings.TrimRight(base, "/"),
		Key:  key,
		HTTP: &http.Client{Timeout: apiTimeout},
	}
}

// SocketStartRequest is the Socket Start v2 request body.
type SocketStartRequest struct {
	Classifications []string `json:"classifications"`
	Types           []string `json:"types,omitempty"`
	// Test is "no" (default) or "including".
	Test    string `json:"test,omitempty"`
	AppName string `json:"appName,omitempty"`
	// FormatMode is "raw" (default: JMA telegrams as delivered) or "json"
	// (DMDATA.JP's JSON conversion).
	FormatMode string `json:"formatMode,omitempty"`
}

// SocketID is a WebSocket ID. The docs type it Integer|String<Integer>, so
// it's kept as its decimal string form.
type SocketID string

func (id *SocketID) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*id = SocketID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*id = SocketID(n.String())
	return nil
}

// SocketStartResponse is the successful Socket Start v2 response.
type SocketStartResponse struct {
	ResponseID string `json:"responseId"`
	Status     string `json:"status"`
	Ticket     string `json:"ticket"`
	WebSocket  struct {
		ID         SocketID `json:"id"`
		URL        string   `json:"url"`
		Protocol   []string `json:"protocol"`
		Expiration int      `json:"expiration"`
	} `json:"websocket"`
	Classifications []string `json:"classifications"`
	Test            string   `json:"test"`
	Types           []string `json:"types"`
	Formats         []string `json:"formats"`
	AppName         *string  `json:"appName"`
}

// APIError is a non-ok REST response.
type APIError struct {
	// HTTPStatus is the HTTP status of the response.
	HTTPStatus int
	// Code and Message come from the body's error object when present.
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("dmdata api: HTTP %d: %s (code %d)", e.HTTPStatus, e.Message, e.Code)
}

type errorBody struct {
	Status string `json:"status"`
	Error  struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// StartSocket calls Socket Start v2 and returns the issued ticket/URL. The
// ticket is single-use and expires after websocket.expiration seconds (300).
func (a *API) StartSocket(ctx context.Context, req SocketStartRequest) (*SocketStartResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	respBody, err := a.do(ctx, http.MethodPost, "/v2/socket", body)
	if err != nil {
		return nil, err
	}
	var resp SocketStartResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("dmdata api: decoding socket start response: %w", err)
	}
	if resp.Status != "ok" {
		return nil, fmt.Errorf("dmdata api: socket start returned status %q", resp.Status)
	}
	if resp.WebSocket.URL == "" {
		return nil, fmt.Errorf("dmdata api: socket start returned no websocket url")
	}
	return &resp, nil
}

// CloseSocket calls Socket Close v2 to release a connection slot.
func (a *API) CloseSocket(ctx context.Context, id SocketID) error {
	if _, err := strconv.ParseInt(string(id), 10, 64); err != nil {
		return fmt.Errorf("dmdata api: invalid socket id %q", id)
	}
	_, err := a.do(ctx, http.MethodDelete, "/v2/socket/"+string(id), nil)
	return err
}

func (a *API) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.Base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(a.Key+":")))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dmdata api: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("dmdata api: reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := &APIError{HTTPStatus: resp.StatusCode}
		var eb errorBody
		if json.Unmarshal(respBody, &eb) == nil {
			apiErr.Code = eb.Error.Code
			apiErr.Message = eb.Error.Message
		}
		return nil, apiErr
	}
	return respBody, nil
}
