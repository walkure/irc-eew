// Package eewzip packs a finished year of the telegram archive (logdir)
// into one zip file, and is the shared contract between the packer
// (cmd/eew-pack) and the viewer (internal/eewview) for where that zip
// lives and how its entries are named.
//
// Layout: <logdir>/<YYYY>.zip next to <logdir>/<YYYY>/, holding entries
// named exactly like the loose files' paths, "YYYY/MM/DD/<file>". Every
// entry's bytes are the archived file's bytes unchanged (deflated inside
// the zip, CRC-checked on read), so packing never alters what was
// received.
//
// A year with telegrams still arriving is not packed (except its legacy
// WNI telegrams, which can no longer arrive): the notifier keeps writing
// loose files, and the viewer reads loose files and the zip together, a
// name present in both counting once.
package eewzip

import (
	"fmt"
	"regexp"
	"strconv"
)

var (
	zipNameRE   = regexp.MustCompile(`^(\d{4})\.zip$`)
	entryNameRE = regexp.MustCompile(`^(\d{4})/(\d{2})/(\d{2})/([^/]+)$`)
)

// ZipName is the file name of year's zip inside logdir.
func ZipName(year int) string { return fmt.Sprintf("%04d.zip", year) }

// ParseZipName returns the year of a zip file name made by ZipName.
func ParseZipName(name string) (year int, ok bool) {
	m := zipNameRE.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	y, _ := strconv.Atoi(m[1])
	return y, true
}

// EntryName is the name of the zip entry for file in the YYYY/MM/DD
// directory of the given date.
func EntryName(year, month, day int, file string) string {
	return fmt.Sprintf("%04d/%02d/%02d/%s", year, month, day, file)
}

// ParseEntryName splits an entry name made by EntryName.
func ParseEntryName(name string) (year, month, day int, file string, ok bool) {
	m := entryNameRE.FindStringSubmatch(name)
	if m == nil {
		return 0, 0, 0, "", false
	}
	year, _ = strconv.Atoi(m[1])
	month, _ = strconv.Atoi(m[2])
	day, _ = strconv.Atoi(m[3])
	return year, month, day, m[4], true
}
