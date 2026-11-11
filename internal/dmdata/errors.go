package dmdata

import (
	"errors"
	"fmt"
	"strings"
)

// WebSocket v2 error codes, from the `error` message table at
// https://dmdata.jp/docs/reference/api/v2/websocket/#type-error
const (
	CodeMissingParameter   = 4400 // required parameter missing at start
	CodeInvalidTicket      = 4404 // no valid ticket at start
	CodeConnectionLimit    = 4409 // connection limit reached at start
	CodeServerRestart      = 4503 // internal error; server app restarting
	CodePingIDMismatch     = 4640 // our pong carried the wrong pingId
	CodeInvalidClientData  = 4641 // we sent something invalid (non-JSON...)
	CodeContractTerminated = 4807 // the only connected classification was cancelled
	CodeClosedByUser       = 4808 // the user explicitly closed the socket
	CodePingTimeout        = 4891 // we didn't answer pings in time
)

// Severity says what a caller should do after RunSession fails.
type Severity int

const (
	// Transient errors are retried with backoff.
	Transient Severity = iota
	// ServerRestart is the service's own restart (incl. the monthly
	// maintenance): an expected disconnection, retried like Transient but
	// not worth a warning.
	ServerRestart
	// Fatal errors can't be fixed by retrying (bad key, no contract,
	// missing permission, contract terminated...).
	Fatal
)

func (s Severity) String() string {
	switch s {
	case Transient:
		return "transient"
	case ServerRestart:
		return "server-restart"
	case Fatal:
		return "fatal"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// Classify maps an error returned by RunSession to a Severity.
//
// REST errors only reach here from Socket Start (RunSession merely logs
// Socket List/Close failures), where:
//   - 400 means our own request is invalid (e.g. app-name over 24 bytes,
//     more than 30 types, an unknown format-mode): a config problem.
//   - 401 (bad key), 402 (no contract), 403 (missing permission or a
//     disallowed source IP) need a human.
//   - 409 (connection limit), 429 (rate limit), 5xx are retried.
func Classify(err error) Severity {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatus {
		case 400, 401, 402, 403:
			return Fatal
		default:
			return Transient
		}
	}
	var grantErr *GrantError
	if errors.As(err, &grantErr) {
		return Fatal
	}
	var se *ServerError
	if errors.As(err, &se) {
		switch se.Code {
		case CodeMissingParameter, CodeContractTerminated:
			return Fatal
		case CodeServerRestart:
			return ServerRestart
		default:
			// 4808 (the user closed this socket, e.g. from the control
			// panel: treated as "reconnect", not "stop"; the user decided
			// this), 4404 (ticket expired/reused: a new one is issued next
			// time),
			// 4409 (limit: stale sockets are cleaned up), 4640/4641/4891
			// (a client-side hiccup; a fresh session recovers), and codes
			// not in the documented table.
			return Transient
		}
	}
	return Transient
}

// GrantError means Socket Start succeeded but didn't grant every
// classification or type asked for — typically because the account has
// no contract for it. Connecting anyway would sit silently receiving
// nothing, so it's treated as fatal.
type GrantError struct {
	MissingClassifications []string
	MissingTypes           []string
}

func (e *GrantError) Error() string {
	var parts []string
	if len(e.MissingClassifications) > 0 {
		parts = append(parts, "classifications "+strings.Join(e.MissingClassifications, ","))
	}
	if len(e.MissingTypes) > 0 {
		parts = append(parts, "types "+strings.Join(e.MissingTypes, ","))
	}
	return "dmdata: socket start did not grant " + strings.Join(parts, " and ")
}

// checkGrant compares what was requested with what Socket Start granted.
// A null types list in the response means "every type of the granted
// classifications", which covers any requested type.
func checkGrant(req SocketStartRequest, resp *SocketStartResponse) error {
	var e GrantError
	for _, c := range req.Classifications {
		if !contains(resp.Classifications, c) {
			e.MissingClassifications = append(e.MissingClassifications, c)
		}
	}
	if resp.Types != nil {
		for _, t := range req.Types {
			if strings.HasPrefix(t, "!") {
				continue // an exclusion, not something to be granted
			}
			if !contains(resp.Types, t) {
				e.MissingTypes = append(e.MissingTypes, t)
			}
		}
	}
	if len(e.MissingClassifications) == 0 && len(e.MissingTypes) == 0 {
		return nil
	}
	return &e
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
