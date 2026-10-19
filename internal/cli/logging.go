package cli

import (
	"io"
	"log/slog"
	"strings"

	"github.com/sgaunet/vigie/internal/token"
)

// NewLogger returns a text logger writing to stderr at level. Any attribute whose key
// contains "token" is masked, as a second line of defence behind token.Token.
func NewLogger(stderr io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Value.Kind() != slog.KindGroup && strings.Contains(strings.ToLower(a.Key), "token") {
				return slog.String(a.Key, token.Mask(a.Value.String()))
			}
			return a
		},
	}))
}
