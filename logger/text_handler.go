package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// textHandler는 Debug 모드에서 stdout에 사람이 읽기 좋게 출력하는 핸들러입니다.
// 포맷: "timestamp LEVEL CATEGORY[jobId=X key=Y] message (error=...)"
type textHandler struct {
	out   io.Writer
	level slog.Level
	mu    sync.Mutex
	attrs []slog.Attr
	group string
}

func newTextHandler(out io.Writer, level slog.Level) *textHandler {
	return &textHandler{out: out, level: level}
}

func (h *textHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *textHandler) Handle(_ context.Context, r slog.Record) error {
	// 속성 수집
	fields := map[string]any{}
	for _, a := range h.attrs {
		fields[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.Any()
		return true
	})

	category, _ := fields["category"].(string)
	jobID, _ := fields["jobId"].(string)
	stepName, _ := fields["stepName"].(string)
	messageType, _ := fields["messageType"].(string)

	// 프리픽스 구성
	var prefix string
	switch category {
	case CategorySQS:
		if messageType != "" {
			prefix = fmt.Sprintf("SQS[jobId=%s type=%s]", jobID, messageType)
		} else {
			prefix = fmt.Sprintf("SQS[jobId=%s]", jobID)
		}
	case CategoryAnalysis:
		prefix = fmt.Sprintf("ANALYSIS[jobId=%s]", jobID)
	case CategoryJobStep:
		prefix = fmt.Sprintf("STEP[jobId=%s %s]", jobID, stepName)
	case CategoryWorker:
		prefix = "WORKER"
	default:
		prefix = "-"
	}

	// 보조 필드 (duration, error) 추출
	var extras []string
	if v, ok := fields["durationMs"]; ok {
		extras = append(extras, fmt.Sprintf("durationMs=%v", v))
	}
	if v, ok := fields["error"]; ok {
		if errMap, ok := v.(map[string]any); ok {
			if msg, ok := errMap["message"].(string); ok {
				extras = append(extras, fmt.Sprintf("error=%q", msg))
			}
			if code, ok := errMap["code"]; ok {
				extras = append(extras, fmt.Sprintf("code=%v", code))
			}
		} else {
			extras = append(extras, fmt.Sprintf("error=%v", v))
		}
	}

	timestamp := r.Time.Format("2006-01-02 15:04:05")
	level := r.Level.String()

	var line strings.Builder
	line.WriteString(timestamp)
	line.WriteByte(' ')
	line.WriteString(fmt.Sprintf("%-5s", level))
	line.WriteByte(' ')
	line.WriteString(prefix)
	line.WriteByte(' ')
	line.WriteString(r.Message)
	for _, e := range extras {
		line.WriteByte(' ')
		line.WriteString(e)
	}
	line.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, line.String())
	return err
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	newAttrs = append(newAttrs, h.attrs...)
	newAttrs = append(newAttrs, attrs...)
	return &textHandler{out: h.out, level: h.level, attrs: newAttrs, group: h.group}
}

func (h *textHandler) WithGroup(name string) slog.Handler {
	return &textHandler{out: h.out, level: h.level, attrs: h.attrs, group: name}
}
