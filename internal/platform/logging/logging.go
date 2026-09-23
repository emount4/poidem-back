// Package logging configures structured application logging.
package logging

import (
	"io"
	"log/slog"
)

func New(out io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
