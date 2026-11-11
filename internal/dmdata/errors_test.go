package dmdata

import (
	"errors"
	"fmt"
	"testing"
)

func TestClassify(t *testing.T) {
	api := func(status int) error { return fmt.Errorf("socket start: %w", &APIError{HTTPStatus: status}) }
	ws := func(code int) error { return &ServerError{ErrorMessage{Code: code, Close: true}} }
	cases := []struct {
		name string
		err  error
		want Severity
	}{
		{"400 bad request", api(400), Fatal},
		{"401 bad key", api(401), Fatal},
		{"402 no contract", api(402), Fatal},
		{"403 no permission", api(403), Fatal},
		{"409 limit", api(409), Transient},
		{"429 rate limit", api(429), Transient},
		{"500", api(500), Transient},
		{"503", api(503), Transient},
		{"4400", ws(CodeMissingParameter), Fatal},
		{"4404", ws(CodeInvalidTicket), Transient},
		{"4409", ws(CodeConnectionLimit), Transient},
		{"4503", ws(CodeServerRestart), ServerRestart},
		{"4640", ws(CodePingIDMismatch), Transient},
		{"4641", ws(CodeInvalidClientData), Transient},
		{"4807", ws(CodeContractTerminated), Fatal},
		{"4808", ws(CodeClosedByUser), Transient},
		{"4891", ws(CodePingTimeout), Transient},
		{"unknown ws code", ws(4999), Transient},
		{"grant", &GrantError{MissingTypes: []string{"VXSE45"}}, Fatal},
		{"network", errors.New("dial tcp: connection refused"), Transient},
	}
	for _, c := range cases {
		if got := Classify(c.err); got != c.want {
			t.Errorf("%s: Classify = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCheckGrant(t *testing.T) {
	req := SocketStartRequest{Classifications: []string{"eew.forecast"}, Types: []string{"VXSE45", "!VXSE44"}}
	ok := &SocketStartResponse{Classifications: []string{"eew.forecast"}, Types: []string{"VXSE45", "!VXSE44"}}
	if err := checkGrant(req, ok); err != nil {
		t.Errorf("full grant: %v", err)
	}
	allTypes := &SocketStartResponse{Classifications: []string{"eew.forecast"}}
	if err := checkGrant(req, allTypes); err != nil {
		t.Errorf("null types (all types): %v", err)
	}
	var ge *GrantError
	if err := checkGrant(req, &SocketStartResponse{Classifications: []string{"telegram.earthquake"}, Types: []string{"VXSE45"}}); !errors.As(err, &ge) ||
		len(ge.MissingClassifications) != 1 || ge.MissingClassifications[0] != "eew.forecast" {
		t.Errorf("missing classification: %v", err)
	}
	if err := checkGrant(req, &SocketStartResponse{Classifications: []string{"eew.forecast"}, Types: []string{"VXSE44"}}); !errors.As(err, &ge) ||
		len(ge.MissingTypes) != 1 || ge.MissingTypes[0] != "VXSE45" {
		t.Errorf("missing type: %v", err)
	}
}
