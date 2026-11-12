package app

import (
	"log/slog"

	"github.com/walkure/irc-eew/internal/dmdata"
	"github.com/walkure/irc-eew/internal/eew"
	"github.com/walkure/irc-eew/internal/eewmsg"
	"github.com/walkure/irc-eew/internal/notify"
	"github.com/walkure/irc-eew/internal/slack"
)

// notifiedType is the only telegram type that is notified. VXSE44 and
// others are archived but not processed (see CLAUDE.md, DMDATA移行).
const notifiedType = "VXSE45"

// Processor turns a received DMDATA data message into the notification
// text/title and the Slack hooks to send it to. It does no network I/O, so
// it's cheaply testable. Not safe for concurrent use (the feed calls it
// from its single read goroutine).
type Processor struct {
	dispatcher *notify.Dispatcher
	origins    *eew.OriginCache
}

// NewProcessor creates a Processor that selects hooks with dispatcher.
func NewProcessor(dispatcher *notify.Dispatcher) *Processor {
	return &Processor{dispatcher: dispatcher, origins: eew.NewOriginCache(0)}
}

// ProcessResult is everything the caller needs to notify Slack and IRC.
type ProcessResult struct {
	Event eew.Event
	Text  string
	Title string
	Hooks []slack.Hook
}

// Process parses m as a VXSE45 report and formats it. ok is false when m
// is not something to notify: another telegram type or a non-XML payload,
// an unparsable document, or a 訓練/試験 (non-通常) report. Each case is
// logged; the raw message has already been archived by then.
func (p *Processor) Process(m *dmdata.DataMessage) (result ProcessResult, ok bool) {
	format := dmdata.StrOrEmpty(m.Format)
	if m.Head.Type != notifiedType || format != "xml" {
		slog.Warn("dmdata data not notified: unexpected type or format",
			"id", m.ID, "type", m.Head.Type, "format", format)
		return ProcessResult{}, false
	}
	body, err := m.DecodeBody()
	if err != nil {
		slog.Error("dmdata data not notified: body undecodable", "id", m.ID, "error", err)
		return ProcessResult{}, false
	}
	r, err := eew.ParseVXSE45(body)
	if err != nil {
		slog.Error("dmdata data not notified: VXSE45 unparsable", "id", m.ID, "error", err)
		return ProcessResult{}, false
	}
	for _, w := range r.Warnings {
		slog.Warn("VXSE45 value not understood", "id", m.ID, "event_id", r.EventID, "serial", r.Serial, "detail", w)
	}
	if r.Status != "通常" {
		slog.Warn("VXSE45 not notified: not a 通常 (operational) report",
			"id", m.ID, "event_id", r.EventID, "status", r.Status)
		return ProcessResult{}, false
	}

	origin := p.origins.Observe(r)
	text, title := eewmsg.FormatReport(r, origin)
	ev := r.Event()
	return ProcessResult{
		Event: ev,
		Text:  text,
		Title: title,
		Hooks: p.dispatcher.HooksFor(ev),
	}, true
}
