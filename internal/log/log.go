package log

import (
	"io"
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	initOnce   sync.Once
	global     zerolog.Logger
	configured bool
)

// Configure sets up the global logger used across the CLI.
func Configure(level string, out io.Writer) {
	initOnce.Do(func() {
		if out == nil {
			out = os.Stderr
		}

		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
		zerolog.TimestampFieldName = "ts"

		lvl := parseLevel(level)
		logger := zerolog.New(out).With().Timestamp().CallerWithSkipFrameCount(3).Logger().Level(lvl)
		global = logger
		log.Logger = logger
		configured = true
	})
}

// L returns the configured logger. Configure must be called before first use,
// otherwise a stderr logger with info level is returned.
func L() *zerolog.Logger {
	if !configured {
		Configure("", nil)
	}
	return &global
}

func parseLevel(s string) zerolog.Level {
	if s == "" {
		if env := os.Getenv("JK_LOG"); env != "" {
			s = env
		}
	}

	switch strings.ToLower(s) {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "info", "":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}
