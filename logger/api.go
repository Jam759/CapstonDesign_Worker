package logger

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"time"
	"worker_GoVer/apperrors"
	"worker_GoVer/metrics"
)

type errorInfo struct {
	Message    string `json:"message"`
	Code       string `json:"code,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Retryable  bool   `json:"retryable,omitempty"`
}

func errorAttr(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	info := errorInfo{Message: err.Error()}
	var ae *apperrors.AnalysisError
	if errors.As(err, &ae) {
		info.Code = string(ae.Code)
		info.HTTPStatus = ae.HTTPStatus
		info.Retryable = ae.Retryable
	}
	return slog.Any("error", map[string]any{
		"message":    info.Message,
		"code":       info.Code,
		"httpStatus": info.HTTPStatus,
		"retryable":  info.Retryable,
	})
}

func contextAttrs(ctx context.Context) []slog.Attr {
	var attrs []slog.Attr
	if tid := TraceIDFromContext(ctx); tid != "" {
		attrs = append(attrs, slog.String("traceId", tid))
	}
	if jid := JobIDFromContext(ctx); jid != "" {
		attrs = append(attrs, slog.String("jobId", jid))
	}
	return attrs
}

func callerAttrs(skip int) []slog.Attr {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return nil
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return nil
	}
	full := fn.Name()
	className := ""
	method := full
	if i := strings.LastIndex(full, "/"); i >= 0 {
		full = full[i+1:]
	}
	if i := strings.Index(full, "."); i >= 0 {
		className = full[:i]
		method = full[i+1:]
	}
	return []slog.Attr{
		slog.String("className", className),
		slog.String("method", method),
	}
}

func mergeAttrs(groups ...[]slog.Attr) []slog.Attr {
	merged := make([]slog.Attr, 0)
	indexByKey := make(map[string]int)

	for _, group := range groups {
		for _, attr := range group {
			if attr.Key == "" {
				continue
			}
			if idx, exists := indexByKey[attr.Key]; exists {
				merged[idx] = attr
				continue
			}
			indexByKey[attr.Key] = len(merged)
			merged = append(merged, attr)
		}
	}

	return merged
}

func attrString(attrs []slog.Attr, key, fallback string) string {
	for i := len(attrs) - 1; i >= 0; i-- {
		if attrs[i].Key != key {
			continue
		}
		value := attrs[i].Value
		switch value.Kind() {
		case slog.KindString:
			if text := strings.TrimSpace(value.String()); text != "" {
				return text
			}
		default:
			if text := strings.TrimSpace(value.String()); text != "" {
				return text
			}
		}
	}
	return fallback
}

// logAttrsSkip is the core logging function. skip is the number of frames to skip for caller info.
func logAttrsSkip(ctx context.Context, skip int, level slog.Level, msg string, attrs ...slog.Attr) {
	if global == nil {
		return
	}
	all := mergeAttrs(contextAttrs(ctx), callerAttrs(skip), attrs)
	global.LogAttrs(ctx, level, msg, all...)
}

// logAttrs emits a log with skip=4: logAttrsSkip→logAttrs→pkg_level_fn→user_code.
func logAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	logAttrsSkip(ctx, 4, level, msg, attrs...)
}

// ── Package-level basics ─────────────────────────────────────────────────────

func Trace(ctx context.Context, msg string, attrs ...slog.Attr) {
	logAttrs(ctx, LevelTrace, msg, attrs...)
}

func Debug(ctx context.Context, msg string, attrs ...slog.Attr) {
	logAttrs(ctx, slog.LevelDebug, msg, attrs...)
}

func Info(ctx context.Context, msg string, attrs ...slog.Attr) {
	logAttrs(ctx, slog.LevelInfo, msg, attrs...)
}

func Warn(ctx context.Context, msg string, attrs ...slog.Attr) {
	logAttrs(ctx, slog.LevelWarn, msg, attrs...)
}

func Error(ctx context.Context, msg string, err error, attrs ...slog.Attr) {
	if err != nil {
		attrs = append(attrs, errorAttr(err))
	}
	logAttrs(ctx, slog.LevelError, msg, attrs...)
}

// ── ComponentLogger ──────────────────────────────────────────────────────────

// ComponentLogger is a package-scoped logger that automatically appends a component label.
type ComponentLogger struct {
	component string
}

// WithComponent returns a ComponentLogger for the given component name.
func WithComponent(component string) *ComponentLogger {
	return &ComponentLogger{component: component}
}

func (c *ComponentLogger) compAttr() slog.Attr {
	return slog.String("component", c.component)
}

func (c *ComponentLogger) Trace(ctx context.Context, msg string, attrs ...slog.Attr) {
	logAttrsSkip(ctx, 3, LevelTrace, msg, append([]slog.Attr{c.compAttr()}, attrs...)...)
}

func (c *ComponentLogger) Debug(ctx context.Context, msg string, attrs ...slog.Attr) {
	logAttrsSkip(ctx, 3, slog.LevelDebug, msg, append([]slog.Attr{c.compAttr()}, attrs...)...)
}

func (c *ComponentLogger) Info(ctx context.Context, msg string, attrs ...slog.Attr) {
	logAttrsSkip(ctx, 3, slog.LevelInfo, msg, append([]slog.Attr{c.compAttr()}, attrs...)...)
}

// Warn logs at WARN level. err may be nil.
func (c *ComponentLogger) Warn(ctx context.Context, msg string, err error, attrs ...slog.Attr) {
	if err != nil {
		attrs = append(attrs, errorAttr(err))
	}
	logAttrsSkip(ctx, 3, slog.LevelWarn, msg, append([]slog.Attr{c.compAttr()}, attrs...)...)
}

// Error logs at ERROR level. err may be nil.
func (c *ComponentLogger) Error(ctx context.Context, msg string, err error, attrs ...slog.Attr) {
	if err != nil {
		attrs = append(attrs, errorAttr(err))
	}
	logAttrsSkip(ctx, 3, slog.LevelError, msg, append([]slog.Attr{c.compAttr()}, attrs...)...)
}

// ── SQS domain helpers ────────────────────────────────────────────────────────

func (c *ComponentLogger) SQSReceived(ctx context.Context, jobID, messageType string, extra ...slog.Attr) {
	metrics.RecordSQSReceived(messageType)
	attrs := append([]slog.Attr{
		c.compAttr(),
		slog.String("category", CategorySQS),
		slog.String("eventType", EventSQSReceived),
		slog.String("jobId", jobID),
		slog.String("messageType", messageType),
	}, extra...)
	logAttrsSkip(ctx, 3, slog.LevelInfo, "SQS message received", attrs...)
}

func (c *ComponentLogger) SQSProcessed(ctx context.Context, jobID, messageType string, durationMs int64, extra ...slog.Attr) {
	metrics.RecordSQSFinished(messageType, "processed", durationMs)
	attrs := append([]slog.Attr{
		c.compAttr(),
		slog.String("category", CategorySQS),
		slog.String("eventType", EventSQSProcessed),
		slog.String("jobId", jobID),
		slog.String("messageType", messageType),
		slog.Int64("durationMs", durationMs),
	}, extra...)
	logAttrsSkip(ctx, 3, slog.LevelInfo, "SQS message processed", attrs...)
}

func (c *ComponentLogger) SQSFailed(ctx context.Context, jobID, messageType string, err error, durationMs int64, extra ...slog.Attr) {
	metrics.RecordSQSFinished(messageType, "failed", durationMs)
	attrs := []slog.Attr{
		c.compAttr(),
		slog.String("category", CategorySQS),
		slog.String("eventType", EventSQSFailed),
		slog.String("jobId", jobID),
		slog.String("messageType", messageType),
		slog.Int64("durationMs", durationMs),
	}
	if err != nil {
		attrs = append(attrs, errorAttr(err))
	}
	logAttrsSkip(ctx, 3, slog.LevelError, "SQS message failed", append(attrs, extra...)...)
}

// ── Analysis domain helpers ───────────────────────────────────────────────────

func (c *ComponentLogger) AnalysisStarted(ctx context.Context, jobID string, extra ...slog.Attr) {
	metrics.RecordAnalysisStarted(attrString(extra, "analysisType", "unknown"))
	attrs := append([]slog.Attr{
		c.compAttr(),
		slog.String("category", CategoryAnalysis),
		slog.String("eventType", EventAnalysisStarted),
		slog.String("jobId", jobID),
	}, extra...)
	logAttrsSkip(ctx, 3, slog.LevelInfo, "analysis started", attrs...)
}

func (c *ComponentLogger) AnalysisCompleted(ctx context.Context, jobID string, durationMs int64, extra ...slog.Attr) {
	metrics.RecordAnalysisFinished(attrString(extra, "analysisType", "unknown"), "completed", durationMs)
	attrs := append([]slog.Attr{
		c.compAttr(),
		slog.String("category", CategoryAnalysis),
		slog.String("eventType", EventAnalysisCompleted),
		slog.String("jobId", jobID),
		slog.Int64("durationMs", durationMs),
	}, extra...)
	logAttrsSkip(ctx, 3, slog.LevelInfo, "analysis completed", attrs...)
}

func (c *ComponentLogger) AnalysisFailed(ctx context.Context, jobID string, err error, durationMs int64, extra ...slog.Attr) {
	metrics.RecordAnalysisFinished(attrString(extra, "analysisType", "unknown"), "failed", durationMs)
	attrs := []slog.Attr{
		c.compAttr(),
		slog.String("category", CategoryAnalysis),
		slog.String("eventType", EventAnalysisFailed),
		slog.String("jobId", jobID),
		slog.Int64("durationMs", durationMs),
	}
	if err != nil {
		attrs = append(attrs, errorAttr(err))
	}
	logAttrsSkip(ctx, 3, slog.LevelError, "analysis failed", append(attrs, extra...)...)
}

// ── Step timer ────────────────────────────────────────────────────────────────

// StepStart records step start and returns a timer for recording completion or failure.
func (c *ComponentLogger) StepStart(ctx context.Context, stepName, jobID string, extra ...slog.Attr) *StepTimer {
	metrics.RecordStepStarted(stepName)
	attrs := append([]slog.Attr{
		c.compAttr(),
		slog.String("category", CategoryJobStep),
		slog.String("eventType", EventStepStarted),
		slog.String("jobId", jobID),
		slog.String("stepName", stepName),
	}, extra...)
	logAttrsSkip(ctx, 3, slog.LevelDebug, stepName+" started", attrs...)
	return &StepTimer{ctx: ctx, component: c.component, stepName: stepName, jobID: jobID, startAt: time.Now()}
}

// StepTimer tracks duration and outcome of a single job step.
type StepTimer struct {
	ctx       context.Context
	component string
	stepName  string
	jobID     string
	startAt   time.Time
}

func (s *StepTimer) Complete(extra ...slog.Attr) {
	if s == nil {
		return
	}
	durationMs := time.Since(s.startAt).Milliseconds()
	metrics.RecordStepFinished(s.stepName, "completed", durationMs)
	attrs := append([]slog.Attr{
		slog.String("component", s.component),
		slog.String("category", CategoryJobStep),
		slog.String("eventType", EventStepCompleted),
		slog.String("jobId", s.jobID),
		slog.String("stepName", s.stepName),
		slog.Int64("durationMs", durationMs),
	}, extra...)
	logAttrsSkip(s.ctx, 3, slog.LevelDebug, s.stepName+" completed", attrs...)
}

func (s *StepTimer) Fail(err error, extra ...slog.Attr) {
	if s == nil {
		return
	}
	durationMs := time.Since(s.startAt).Milliseconds()
	metrics.RecordStepFinished(s.stepName, "failed", durationMs)
	attrs := []slog.Attr{
		slog.String("component", s.component),
		slog.String("category", CategoryJobStep),
		slog.String("eventType", EventStepFailed),
		slog.String("jobId", s.jobID),
		slog.String("stepName", s.stepName),
		slog.Int64("durationMs", durationMs),
	}
	if err != nil {
		attrs = append(attrs, errorAttr(err))
	}
	logAttrsSkip(s.ctx, 3, slog.LevelError, s.stepName+" failed", append(attrs, extra...)...)
}

// ── Worker events ─────────────────────────────────────────────────────────────

func (c *ComponentLogger) WorkerEvent(ctx context.Context, eventType, msg string, extra ...slog.Attr) {
	metrics.RecordWorkerEvent(eventType)
	attrs := append([]slog.Attr{
		c.compAttr(),
		slog.String("category", CategoryWorker),
		slog.String("eventType", eventType),
	}, extra...)
	logAttrsSkip(ctx, 3, slog.LevelInfo, msg, attrs...)
}
