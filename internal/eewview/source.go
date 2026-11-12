package eewview

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/walkure/irc-eew/internal/decoder"
	"github.com/walkure/irc-eew/internal/dmdata"
	"github.com/walkure/irc-eew/internal/eew"
)

// File names in a <DataDir>/YYYY/MM/DD directory:
//
//	legacy WNI:   <eq_id>.<warn_num>                       (internal/eewlog)
//	DMDATA:       <EventID>.<Serial>_<received>_<id>.json  (internal/rawarchive)
//	DMDATA (no EventID in the envelope): <received>_<id>.json
//
// The detail page's ?name= is the file name without ".json".
var (
	legacyNameRE   = regexp.MustCompile(`^(\d{14})\.(\d{1,4})$`)
	dmdataNameRE   = regexp.MustCompile(`^(\d{14})\.(\d{1,4})_(\d{14}\.\d{3})_(x?[0-9a-fA-F]{1,129})$`)
	dmdataBareRE   = regexp.MustCompile(`^(\d{14}\.\d{3})_(x?[0-9a-fA-F]{1,129})$`)
	receivedLayout = "20060102150405.000"
)

func isLegacyName(name string) bool { return legacyNameRE.MatchString(name) }

func isDMDATAName(name string) bool {
	return dmdataNameRE.MatchString(name) || dmdataBareRE.MatchString(name)
}

// validName reports whether name is a ?name= this viewer generates.
func validName(name string) bool { return isLegacyName(name) || isDMDATAName(name) }

// recordPath maps a validated name to its file: both layouts keep the
// date in the first 8 digits of the name.
func recordPath(dataDir, name string) (string, error) {
	if !validName(name) {
		return "", fmt.Errorf("eewview: invalid name %q", name)
	}
	file := name
	if isDMDATAName(name) {
		file += ".json"
	}
	full := filepath.Join(dataDir, name[0:4], name[4:6], name[6:8], file)

	// Defense in depth: the name patterns already rule out traversal.
	base, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(resolved, base) {
		return "", fmt.Errorf("eewview: resolved path escapes data dir")
	}
	return full, nil
}

// telegramPath is kept for the legacy name form (see recordPath).
func telegramPath(dataDir, name string) (string, error) { return recordPath(dataDir, name) }

// loadDay reads every archived report in one day directory, both formats,
// sorted by EventID, then Serial, then receive time. A DMDATA
// cancellation carries no origin time; it takes the one from the latest
// earlier report of the same EventID in the day, like the notifier does.
func loadDay(dayDir string) ([]*record, error) {
	entries, err := os.ReadDir(dayDir)
	if err != nil {
		return nil, err
	}
	var recs []*record
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		file := e.Name()
		switch {
		case isLegacyName(file):
			body, err := os.ReadFile(filepath.Join(dayDir, file))
			if err != nil {
				continue
			}
			recs = append(recs, legacyRecord(file, decoder.Decode(body)))
		case strings.HasSuffix(file, ".json") && isDMDATAName(strings.TrimSuffix(file, ".json")):
			recs = append(recs, readDMDATA(filepath.Join(dayDir, file)))
		}
	}
	sort.SliceStable(recs, func(i, j int) bool {
		a, b := recs[i], recs[j]
		if a.EventID != b.EventID {
			return a.EventID < b.EventID
		}
		if a.Serial != b.Serial {
			return a.Serial < b.Serial
		}
		return a.received.Before(b.received)
	})

	lastOrigin := map[string][2]string{}
	for _, r := range recs {
		if r.OriginFull != "" {
			lastOrigin[r.EventID] = [2]string{r.OriginFull, r.OriginClock}
		} else if o, ok := lastOrigin[r.EventID]; ok && r.Cancelled {
			r.OriginFull, r.OriginClock = o[0], o[1]
		}
	}
	return recs, nil
}

// readDMDATA turns one archived DMDATA data message into a record. A file
// that isn't a readable VXSE45 telegram still yields a record (marked
// Unparsable) so it stays visible in the listing.
func readDMDATA(path string) *record {
	name := strings.TrimSuffix(filepath.Base(path), ".json")
	received := receivedFromName(name)
	fallback := &record{Name: name, Unparsable: true, received: received}
	if m := dmdataNameRE.FindStringSubmatch(name); m != nil {
		fallback.EventID = m[1]
		fmt.Sscanf(m[2], "%d", &fallback.Serial)
	} else {
		fallback.EventID = received.Format("20060102150405")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var msg dmdata.DataMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fallback
	}
	if msg.Head.Type != "VXSE45" || dmdata.StrOrEmpty(msg.Format) != "xml" {
		return fallback
	}
	body, err := msg.DecodeBody()
	if err != nil {
		return fallback
	}
	rep, err := eew.ParseVXSE45(body)
	if err != nil {
		return fallback
	}
	return dmdataRecord(name, received, rep)
}

func receivedFromName(name string) time.Time {
	var s string
	if m := dmdataNameRE.FindStringSubmatch(name); m != nil {
		s = m[3]
	} else if m := dmdataBareRE.FindStringSubmatch(name); m != nil {
		s = m[1]
	}
	t, _ := time.ParseInLocation(receivedLayout, s, time.Local)
	return t
}
