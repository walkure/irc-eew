// Command eew-pack packs one year of the telegram archive (logdir) into
// <logdir>/<YYYY>.zip (see internal/eewzip). It only adds a zip: the
// loose files are left in place, to be removed by hand once the zip has
// been checked.
//
//	eew-pack -dir /eewlog -year 2025
//	eew-pack -dir /eewlog -year 2026 -legacy-only   # a year still receiving DMDATA
//	eew-pack -dir /eewlog -year 2025 -out /tmp/2025.zip
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/walkure/irc-eew/internal/eewzip"
)

func main() {
	dir := flag.String("dir", "", "archive root (logdir)")
	year := flag.Int("year", 0, "year to pack")
	out := flag.String("out", "", "zip to write (default <dir>/<year>.zip); an existing zip there is merged")
	legacyOnly := flag.Bool("legacy-only", false, "pack only legacy WNI telegrams (leave *.json loose)")
	flag.Parse()
	if *dir == "" || *year == 0 || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}

	res, err := eewzip.Pack(eewzip.PackOptions{Dir: *dir, Year: *year, Out: *out, LegacyOnly: *legacyOnly})
	if err != nil {
		fmt.Fprintln(os.Stderr, "eew-pack:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s: %d entries (kept %d, added %d, already packed %d, json left loose %d)\n",
		res.Out, res.Entries, res.Kept, res.Added, res.AlreadyPacked, res.SkippedJSON)
	if len(res.Irregular) > 0 {
		fmt.Printf("not packed (outside the YYYY/MM/DD/<file> layout): %d\n", len(res.Irregular))
		for _, p := range res.Irregular {
			fmt.Println("  " + p)
		}
	}
}
