package logger

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// NewSlogHandler returns a slog.Handler that forwards records to the provided Logger.
// If logger is nil, it returns nil.
func NewSlogHandler(l *Logger) slog.Handler {
	if l == nil {
		return nil
	}
	return &slogAdapter{
		log:    l,
		groups: nil,
		attrs:  nil,
	}
}

type slogAdapter struct {
	log    *Logger
	groups []string
	attrs  []slog.Attr
}

func (h *slogAdapter) Enabled(_ context.Context, level slog.Level) bool {
	if h.log == nil {
		return false
	}
	mapped := slogLevelToLoggerLevel(level)
	return mapped >= h.log.GetLevel()
}

func (h *slogAdapter) Handle(_ context.Context, record slog.Record) error {
	if h.log == nil {
		return nil
	}

	message := record.Message

	combined := make([]slog.Attr, 0, len(h.attrs)+record.NumAttrs())
	combined = append(combined, h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		combined = append(combined, attr)
		return true
	})

	attrText := formatAttrs(combined, h.groups)
	if attrText != "" {
		if message != "" {
			message = fmt.Sprintf("%s %s", message, attrText)
		} else {
			message = attrText
		}
	}

	switch {
	case record.Level >= slog.LevelError:
		h.log.Error("%s", message)
	case record.Level >= slog.LevelWarn:
		h.log.Warn("%s", message)
	case record.Level >= slog.LevelInfo:
		h.log.Info("%s", message)
	default:
		h.log.Debug("%s", message)
	}

	return nil
}

func (h *slogAdapter) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	newAttrs = append(newAttrs, h.attrs...)
	newAttrs = append(newAttrs, attrs...)
	return &slogAdapter{
		log:    h.log,
		groups: append([]string(nil), h.groups...),
		attrs:  newAttrs,
	}
}

func (h *slogAdapter) WithGroup(name string) slog.Handler {
	newGroups := append([]string(nil), h.groups...)
	if name != "" {
		newGroups = append(newGroups, name)
	}
	return &slogAdapter{
		log:    h.log,
		groups: newGroups,
		attrs:  append([]slog.Attr(nil), h.attrs...),
	}
}

func slogLevelToLoggerLevel(level slog.Level) Level {
	switch {
	case level >= slog.LevelError:
		return LevelError
	case level >= slog.LevelWarn:
		return LevelWarn
	case level >= slog.LevelInfo:
		return LevelInfo
	default:
		return LevelDebug
	}
}

func formatAttrs(attrs []slog.Attr, groups []string) string {
	if len(attrs) == 0 {
		return ""
	}

	var builder strings.Builder
	first := true
	for _, attr := range attrs {
		first = writeAttr(&builder, attr, groups, first)
	}

	return builder.String()
}

func writeAttr(builder *strings.Builder, attr slog.Attr, prefix []string, first bool) bool {
	if attr.Equal(slog.Attr{}) {
		return first
	}

	if attr.Value.Kind() == slog.KindGroup {
		groupPrefix := appendKey(prefix, attr.Key)
		for _, nested := range attr.Value.Group() {
			first = writeAttr(builder, nested, groupPrefix, first)
		}
		return first
	}

	key := attr.Key
	if key == "" {
		key = "attr"
	}

	keyParts := appendKey(prefix, key)
	if !first {
		builder.WriteByte(' ')
	}
	fmt.Fprintf(builder, "%s=%v", strings.Join(keyParts, "."), attr.Value)
	return false
}

func appendKey(prefix []string, key string) []string {
	combined := make([]string, 0, len(prefix)+1)
	combined = append(combined, prefix...)
	combined = append(combined, key)
	return combined
}
