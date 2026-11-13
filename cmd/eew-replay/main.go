// Command eew-replay feeds archived telegram files through the same
// parse -> format -> (optionally) Slack-send pipeline the production
// daemon uses, without connecting to any feed. Intended for eyeballing
// message formatting against real historical telegrams.
//
// It accepts both archive formats found under logdir:
//   - DMDATA data messages (*.json, as internal/rawarchive saves them):
//     processed by app.Processor exactly like the live notifier, including
//     carrying a cancellation's origin time over from earlier files of the
//     same EventID (pass the files in delivery order).
//   - legacy WNI code telegrams (anything else): internal/decoder +
//     eewmsg.Format, the pre-DMDATA pipeline.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/walkure/irc-eew/internal/app"
	"github.com/walkure/irc-eew/internal/decoder"
	"github.com/walkure/irc-eew/internal/dmdata"
	"github.com/walkure/irc-eew/internal/eewmsg"
	"github.com/walkure/irc-eew/internal/notify"
	"github.com/walkure/irc-eew/internal/slack"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	webhookURL := flag.String("slack-webhook", "", "if set, POST each formatted message to this Slack incoming-webhook URL")
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: eew-replay [-slack-webhook URL] <telegram-file> [<telegram-file> ...]")
		os.Exit(2)
	}

	var notifier *slack.Notifier
	if *webhookURL != "" {
		notifier = slack.New()
	}
	// No hooks: only the formatting (and the per-EventID state behind it)
	// is wanted from the Processor; sending is done below.
	processor := app.NewProcessor(notify.NewDispatcher(nil, nil))

	for _, path := range flag.Args() {
		body, err := os.ReadFile(path)
		if err != nil {
			slog.Error("reading telegram file", "path", path, "error", err)
			continue
		}

		text, title, ok := format(processor, path, body)
		if !ok {
			continue
		}
		fmt.Printf("=== %s ===\ntitle: %s\ntext:  %s\n\n", path, title, text)

		if notifier != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := notifier.Send(ctx, slack.Hook{Name: "eew-replay", URL: *webhookURL}, text, title)
			cancel()
			if err != nil {
				slog.Error("slack send failed", "path", path, "error", err)
			} else {
				slog.Info("sent to Slack", "path", path)
			}
		}
	}
}

// format renders one archived file in whichever format it is.
func format(processor *app.Processor, path string, body []byte) (text, title string, ok bool) {
	if !strings.HasSuffix(path, ".json") {
		text, title = eewmsg.Format(decoder.Decode(body))
		return text, title, true
	}
	var m dmdata.DataMessage
	if err := json.Unmarshal(body, &m); err != nil {
		slog.Error("not a DMDATA data message", "path", path, "error", err)
		return "", "", false
	}
	m.Raw = body
	result, ok := processor.Process(&m)
	if !ok {
		// Processor has already logged why (type/format, unparsable, 訓練...).
		slog.Error("not notified by the daemon either", "path", path)
		return "", "", false
	}
	return result.Text, result.Title, true
}
