package irc

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/fluffle/goirc/logging"
)

// slogLogger adapts goirc's logging.Logger interface to slog.
type slogLogger struct{}

func (slogLogger) Debug(format string, args ...interface{}) { slog.Debug(fmt.Sprintf(format, args...)) }
func (slogLogger) Info(format string, args ...interface{})  { slog.Info(fmt.Sprintf(format, args...)) }
func (slogLogger) Warn(format string, args ...interface{})  { slog.Warn(fmt.Sprintf(format, args...)) }
func (slogLogger) Error(format string, args ...interface{}) { slog.Error(fmt.Sprintf(format, args...)) }

var installLoggerOnce sync.Once

// installLogger routes goirc's internal logging (raw socket I/O at Debug
// level, connection lifecycle at Info/Warn/Error) through slog instead of
// its default no-op. logging.SetLogger is process-global (goirc has no
// per-Conn logger), so this only needs to run once regardless of how many
// Connections get created.
func installLogger() {
	installLoggerOnce.Do(func() {
		logging.SetLogger(slogLogger{})
	})
}
