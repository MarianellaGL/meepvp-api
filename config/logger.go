package config

import (
	"log/slog"
	"os"
	"strings"

	"github.com/lmittmann/tint"
)

func SetupLogger(prod bool, level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	var handler slog.Handler
	if prod {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: lvl,
		})
	} else {
		handler = tint.NewHandler(os.Stdout, &tint.Options{
			Level:   lvl,
			NoColor: false,
		})
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}
