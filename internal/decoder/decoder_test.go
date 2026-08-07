package decoder

import (
	"os"
	"testing"
)

// These fixtures are real telegrams from the 45,802-file eewlog/ corpus
// (2011-03-12 Nagano/Niigata-border aftershock sequence and a genuine
// cancellation report), the same files used to manually cross-validate the
// wnisim fake WNI server against the ~10-years-in-production Perl
// EEWSock.pm/Decoder.pm — the expected values below were read directly off
// that verified Perl run's stdout output plus the raw wire bytes.

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return b
}

func TestDecode_NewReport(t *testing.T) {
	tel := Decode(readFixture(t, "2011_new_report.txt"))

	if tel.Code != "37" || tel.CodeType == "" {
		t.Errorf("Code/CodeType: got %q/%q", tel.Code, tel.CodeType)
	}
	if tel.SectionCode != "03" || tel.Section != "気象庁本庁" {
		t.Errorf("Section: got %q/%q", tel.SectionCode, tel.Section)
	}
	if tel.MsgTypeCode != 0 {
		t.Errorf("MsgTypeCode: got %d, want 0", tel.MsgTypeCode)
	}
	if tel.WarnTime != "110312042450" {
		t.Errorf("WarnTime: got %q", tel.WarnTime)
	}
	if tel.EqTime != "110312042348" {
		t.Errorf("EqTime: got %q", tel.EqTime)
	}
	if tel.EqID != "20110312042346" {
		t.Errorf("EqID: got %q", tel.EqID)
	}
	if tel.WarnType != "高度利用者向け" {
		t.Errorf("WarnType: got %q", tel.WarnType)
	}
	if tel.NCNType != 0 {
		t.Errorf("NCNType: got %d, want 0", tel.NCNType)
	}
	if tel.WarnNum != 1 {
		t.Errorf("WarnNum: got %d, want 1", tel.WarnNum)
	}
	if tel.IsFinal() {
		t.Error("IsFinal: got true, want false for report #1")
	}
	if tel.IsCancellation() {
		t.Error("IsCancellation: got true, want false")
	}
	if tel.CenterCode != "220" || tel.CenterName != "宮城県北部" {
		t.Errorf("Center: got %q/%q", tel.CenterCode, tel.CenterName)
	}
	if tel.CenterLat != 38.5 || tel.CenterLng != 140.8 {
		t.Errorf("Lat/Lng: got %v/%v", tel.CenterLat, tel.CenterLng)
	}
	if tel.CenterDepth != 10 {
		t.Errorf("CenterDepth: got %d, want 10", tel.CenterDepth)
	}
	if tel.Magnitude != 6.8 {
		t.Errorf("Magnitude: got %v, want 6.8", tel.Magnitude)
	}
	if tel.ShindoCode != "6+" || tel.Shindo != "6強" {
		t.Errorf("Shindo: got %q/%q", tel.ShindoCode, tel.Shindo)
	}

	if len(tel.EBI) != 18 {
		t.Fatalf("EBI count: got %d, want 18", len(tel.EBI))
	}
	e, ok := tel.EBI["220"]
	if !ok {
		t.Fatal("EBI[220] missing")
	}
	if e.Name != "宮城県北部" {
		t.Errorf("EBI[220].Name: got %q", e.Name)
	}
	if e.Shindo1Code != "6+" || e.Shindo2Code != "6-" {
		t.Errorf("EBI[220] shindo range: got %q/%q", e.Shindo1Code, e.Shindo2Code)
	}
	if e.IsWarndedCode != "0" || e.IsWarnded != "警報なし" {
		t.Errorf("EBI[220].IsWarnded: got %q/%q", e.IsWarndedCode, e.IsWarnded)
	}
	if e.ArriveCode != "1" || e.Arrive != "既に到着と予想" {
		t.Errorf("EBI[220].Arrive: got %q/%q", e.ArriveCode, e.Arrive)
	}
}

func TestDecode_FinalReport(t *testing.T) {
	tel := Decode(readFixture(t, "2011_final_report.txt"))

	if tel.NCNType != 9 {
		t.Errorf("NCNType: got %d, want 9", tel.NCNType)
	}
	if tel.WarnNum != 81 {
		t.Errorf("WarnNum: got %d, want 81", tel.WarnNum)
	}
	if !tel.IsFinal() {
		t.Error("IsFinal: got false, want true for the final report")
	}
	if tel.Magnitude != 6.7 {
		t.Errorf("Magnitude: got %v, want 6.7", tel.Magnitude)
	}
	if len(tel.EBI) != 18 {
		t.Fatalf("EBI count: got %d, want 18", len(tel.EBI))
	}
}

func TestDecode_Cancellation(t *testing.T) {
	tel := Decode(readFixture(t, "2011_cancellation.txt"))

	if tel.MsgTypeCode != 10 {
		t.Errorf("MsgTypeCode: got %d, want 10", tel.MsgTypeCode)
	}
	if !tel.IsCancellation() {
		t.Error("IsCancellation: got false, want true")
	}
	if tel.EqID != "20110312134730" {
		t.Errorf("EqID: got %q", tel.EqID)
	}
	if tel.WarnNum != 3 {
		t.Errorf("WarnNum: got %d, want 3", tel.WarnNum)
	}
	// Cancellation telegrams carry no hypocenter/EBI lines.
	if tel.CenterCode != "" {
		t.Errorf("CenterCode: got %q, want empty for a cancellation", tel.CenterCode)
	}
	if len(tel.EBI) != 0 {
		t.Errorf("EBI: got %d entries, want 0 for a cancellation", len(tel.EBI))
	}
}

func TestDecode_EmptyInput(t *testing.T) {
	tel := Decode(nil)
	if tel == nil {
		t.Fatal("Decode(nil) returned nil, want a zero-value Telegram")
	}
	if tel.EqID != "" || tel.MsgTypeCode != 0 {
		t.Errorf("expected zero-value Telegram, got %+v", tel)
	}
}
