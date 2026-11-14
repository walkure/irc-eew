package eewview

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/walkure/irc-eew/internal/decoder"
	"github.com/walkure/irc-eew/internal/dmdata"
	"github.com/walkure/irc-eew/internal/eew"
)

// File names in a <DataDir>/YYYY/MM/DD directory:
//
//	legacy WNI:   <eq_id>.<warn_num>                       (written before DMDATA)
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

// nameDate is the archive date of a validated name: both layouts keep it
// in the first 8 digits.
func nameDate(name string) (year, month, day int) {
	year, _ = strconv.Atoi(name[0:4])
	month, _ = strconv.Atoi(name[4:6])
	day, _ = strconv.Atoi(name[6:8])
	return year, month, day
}

// loadDay turns one day's archived telegrams (store.dayFiles), both
// formats, into records sorted by EventID, then Serial, then receive time.
// A DMDATA cancellation carries no origin time; it takes the one from the
// latest earlier report of the same EventID in the day, like the notifier
// does.
func loadDay(files []dayFile) []*record {
	var recs []*record
	for _, f := range files {
		switch {
		case isLegacyName(f.name):
			body, err := f.read()
			if err != nil {
				continue
			}
			recs = append(recs, legacyRecord(f.name, decoder.Decode(body)))
		case strings.HasSuffix(f.name, ".json") && isDMDATAName(strings.TrimSuffix(f.name, ".json")):
			raw, err := f.read()
			recs = append(recs, readDMDATA(strings.TrimSuffix(f.name, ".json"), raw, err))
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
	return recs
}

// readDMDATA turns one archived DMDATA data message (name is the file
// name without .json, raw/readErr what reading it gave) into a record. A
// file that isn't a readable VXSE45 telegram still yields a record
// (marked Unparsable) so it stays visible in the listing.
func readDMDATA(name string, raw []byte, readErr error) *record {
	received := receivedFromName(name)
	fallback := &record{Name: name, Unparsable: true, received: received}
	if m := dmdataNameRE.FindStringSubmatch(name); m != nil {
		fallback.EventID = m[1]
		fmt.Sscanf(m[2], "%d", &fallback.Serial)
	} else {
		fallback.EventID = received.Format("20060102150405")
	}

	if readErr != nil {
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
