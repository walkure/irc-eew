package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walkure/irc-eew/internal/dmdata"
	"github.com/walkure/irc-eew/internal/dmdatasim"
	"github.com/walkure/irc-eew/internal/notify"
	"github.com/walkure/irc-eew/internal/slack"
)

// realMessage loads a data message exactly as DMDATA delivered it.
func realMessage(t *testing.T, name string) *dmdata.DataMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var m dmdata.DataMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m.Raw = raw
	return &m
}

// vxse45Message wraps a VXSE45 XML fixture from internal/eew/testdata the
// way DMDATA delivers it (gzip + base64).
func vxse45Message(t *testing.T, name string) *dmdata.DataMessage {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "eew", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return messageFromXML(t, body, "VXSE45", "xml")
}

func messageFromXML(t *testing.T, body []byte, typ, format string) *dmdata.DataMessage {
	t.Helper()
	raw := dmdatasim.DataMessage(dmdatasim.Telegram{Type: typ, Format: format, Body: body})
	var m dmdata.DataMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m.Raw = raw
	return &m
}

func newTestProcessor() *Processor {
	return NewProcessor(notify.NewDispatcher(
		[]slack.Hook{{Name: "all-1"}},
		[]slack.Hook{{Name: "limited-1"}},
	))
}

func TestProcessor_RealDeliveredMessages(t *testing.T) {
	p := newTestProcessor()

	first, ok := p.Process(realMessage(t, "dmdata_vxse45_first.json"))
	if !ok {
		t.Fatal("first report not processed")
	}
	wantFirst := "2026/11/12 11:35:54 第1報 (2026/11/12 11:35:21発生)" +
		"震央:<http://maps.google.com/maps?q=23.7,123.8|N23.7/E123.8>(石垣島近海)深さ20km 最大:M3.5 震度1"
	if first.Text != wantFirst || first.Title != "高度利用者向け緊急地震速報" {
		t.Errorf("first:\n got  %q / %q\n want %q", first.Text, first.Title, wantFirst)
	}
	if len(first.Hooks) != 2 || first.Event.EventID != "20261112113529" {
		t.Errorf("first: hooks %+v event %+v", first.Hooks, first.Event)
	}

	final, ok := p.Process(realMessage(t, "dmdata_vxse45_final.json"))
	if !ok {
		t.Fatal("final report not processed")
	}
	if !strings.Contains(final.Text, "第4報(最終)") || !final.Event.Final || len(final.Hooks) != 2 {
		t.Errorf("final: %q hooks %+v event %+v", final.Text, final.Hooks, final.Event)
	}
}

func TestProcessor_RoutineReportSkipsLimitedTier(t *testing.T) {
	p := newTestProcessor()
	p.Process(vxse45Message(t, "real_20261112130734_01.xml"))
	r, ok := p.Process(vxse45Message(t, "real_20261112130734_02.xml"))
	if !ok || len(r.Hooks) != 1 || r.Hooks[0].Name != "all-1" {
		t.Errorf("routine report: ok=%v hooks=%+v", ok, r.Hooks)
	}
}

// Two earthquakes' reports interleaving: limited fires once per quake, and
// nothing is mixed up between them.
func TestProcessor_InterleavedQuakes(t *testing.T) {
	p := newTestProcessor()
	seq := []struct {
		name        string
		wantLimited bool
	}{
		{"real_20261112113529_01.xml", true},
		{"real_20261112130734_01.xml", true},
		{"real_20261112113529_02.xml", false},
		{"real_20261112130734_02.xml", false},
		{"real_20261112113529_04.xml", true}, // final
	}
	for _, s := range seq {
		r, ok := p.Process(vxse45Message(t, s.name))
		if !ok {
			t.Fatalf("%s not processed", s.name)
		}
		if got := len(r.Hooks) == 2; got != s.wantLimited {
			t.Errorf("%s: limited fired = %v, want %v", s.name, got, s.wantLimited)
		}
	}
}

// A cancellation has no origin time of its own; the one from the same
// EventID's earlier report is shown, and the XML Serial is kept.
func TestProcessor_CancellationUsesEarlierOriginOfSameEvent(t *testing.T) {
	p := newTestProcessor()
	// JMA's sample series: report 32 (with an origin time) then the
	// cancellation (Serial 32, no Earthquake element).
	earlier := []byte(strings.Replace(
		readXML(t, "jma_sample_77_01_29.xml"), "<Serial>29</Serial>", "<Serial>32</Serial>", 1))
	if _, ok := p.Process(messageFromXML(t, earlier, "VXSE45", "xml")); !ok {
		t.Fatal("earlier report not processed")
	}
	r, ok := p.Process(vxse45Message(t, "jma_sample_77_01_33.xml"))
	if !ok {
		t.Fatal("cancellation not processed")
	}
	want := "2024/04/17 23:17:00 第32報 (2024/04/17 23:14:47発生) 取り消されました"
	if r.Text != want || !r.Event.Cancelled || len(r.Hooks) != 2 {
		t.Errorf("cancellation:\n got  %q (event %+v, %d hooks)\n want %q", r.Text, r.Event, len(r.Hooks), want)
	}
}

func readXML(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "eew", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProcessor_NotNotified(t *testing.T) {
	p := newTestProcessor()
	xml := []byte(readXML(t, "real_20261112113529_01.xml"))
	training := []byte(strings.Replace(string(xml), "<Status>通常</Status>", "<Status>訓練</Status>", 1))
	cases := map[string]*dmdata.DataMessage{
		"other type":   messageFromXML(t, xml, "VXSE44", "xml"),
		"non-xml":      messageFromXML(t, xml, "VXSE45", "a/n"),
		"unparsable":   messageFromXML(t, []byte("<Report><broken>"), "VXSE45", "xml"),
		"訓練 report":    messageFromXML(t, training, "VXSE45", "xml"),
		"bad encoding": {Format: strp("xml"), Encoding: strp("rot13"), Head: dmdata.DataHead{Type: "VXSE45"}},
	}
	for name, m := range cases {
		if _, ok := p.Process(m); ok {
			t.Errorf("%s: processed, want skipped", name)
		}
	}
}

func strp(s string) *string { return &s }
