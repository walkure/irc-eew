// Command eew-replay feeds one or more raw telegram files through the same
// decode -> format -> (optionally) Slack-send pipeline the production
// daemon uses, without needing a live WNI connection. Intended for
// eyeballing message formatting against real historical telegrams (e.g.
// from eewlog/) before the very first genuine live earthquake exercises the
// Go port end-to-end.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/walkure/irc-eew/internal/decoder"
	"github.com/walkure/irc-eew/internal/eewmsg"
	"github.com/walkure/irc-eew/internal/slack"
)

func main() {
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

	for _, path := range flag.Args() {
		body, err := os.ReadFile(path)
		if err != nil {
			log.Printf("%s: %v", path, err)
			continue
		}

		tel := decoder.Decode(body)
		text, title := eewmsg.Format(tel)
		fmt.Printf("=== %s ===\ntitle: %s\ntext:  %s\n\n", path, title, text)

		if notifier != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := notifier.Send(ctx, slack.Hook{Name: "eew-replay", URL: *webhookURL}, text, title)
			cancel()
			if err != nil {
				log.Printf("%s: slack send failed: %v", path, err)
			} else {
				log.Printf("%s: sent to Slack", path)
			}
		}
	}
}
