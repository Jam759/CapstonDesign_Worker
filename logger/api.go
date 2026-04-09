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

// errorInfo는 메인서버 JSON 로그의 error 필드 구조에 맞춥니다.
type errorInfo struct {
	Message    string `json:"message"`
	Code       string `json:"code,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Retryable  bool   `json:"retryable,omitempty"`
}

// errorAttr은 error를 메인서버 포맷의 error 필드로 변환합니다.
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

// contextAttrs는 ctx에서 traceId/jobId를 꺼내 slog.Attr 슬라이스로 반환합니다.
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

// callerAttrs는 로그 호출 지점의 className(패키지명) / method를 추출합니다.
func callerAttrs(skip int) []slog.Attr {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return nil
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return nil
	}
	full := fn.Name() // 예: worker_GoVer/sqs.(*Consumer).handleAnalysisMessage
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

// logAttrs는 최종적으로 slog에 로그를 보냅니다.
func logAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	if global == nil {
		return
	}
	all := mergeAttrs(contextAttrs(ctx), callerAttrs(3), attrs)
	global.LogAttrs(ctx, level, msg, all...)
}

// Info는 INFO 레벨로 로그를 기록합니다.
func Info(ctx context.Context, msg string, attrs ...slog.Attr) {
	logAttrs(ctx, slog.LevelInfo, msg, attrs...)
}

// Warn은 WARN 레벨로 로그를 기록합니다.
func Warn(ctx context.Context, msg string, attrs ...slog.Attr) {
	logAttrs(ctx, slog.LevelWarn, msg, attrs...)
}

// Error는 ERROR 레벨로 로그를 기록합니다. err가 nil이 아니면 error 필드가 추가됩니다.
func Error(ctx context.Context, msg string, err error, attrs ...slog.Attr) {
	if err != nil {
		attrs = append(attrs, errorAttr(err))
	}
	logAttrs(ctx, slog.LevelError, msg, attrs...)
}

// Debug는 DEBUG 레벨로 로그를 기록합니다.
func Debug(ctx context.Context, msg string, attrs ...slog.Attr) {
	logAttrs(ctx, slog.LevelDebug, msg, attrs...)
}

// ============================================================
// SQS 이벤트 헬퍼
// ============================================================

// SQSReceived는 SQS 메시지를 수신했을 때 기록합니다.
func SQSReceived(ctx context.Context, jobID, messageType string, extra ...slog.Attr) {
	metrics.RecordSQSReceived(messageType)
	attrs := []slog.Attr{
		slog.String("category", CategorySQS),
		slog.String("eventType", EventSQSReceived),
		slog.String("jobId", jobID),
		slog.String("messageType", messageType),
	}
	attrs = append(attrs, extra...)
	logAttrs(ctx, slog.LevelInfo, "SQS message received", attrs...)
}

// SQSProcessed는 SQS 메시지 처리를 성공적으로 완료했을 때 기록합니다.
func SQSProcessed(ctx context.Context, jobID, messageType string, durationMs int64, extra ...slog.Attr) {
	metrics.RecordSQSFinished(messageType, "processed", durationMs)
	attrs := []slog.Attr{
		slog.String("category", CategorySQS),
		slog.String("eventType", EventSQSProcessed),
		slog.String("jobId", jobID),
		slog.String("messageType", messageType),
		slog.Int64("durationMs", durationMs),
	}
	attrs = append(attrs, extra...)
	logAttrs(ctx, slog.LevelInfo, "SQS message processed", attrs...)
}

// SQSFailed는 SQS 메시지 처리 실패 시 기록합니다.
func SQSFailed(ctx context.Context, jobID, messageType string, err error, durationMs int64, extra ...slog.Attr) {
	metrics.RecordSQSFinished(messageType, "failed", durationMs)
	attrs := []slog.Attr{
		slog.String("category", CategorySQS),
		slog.String("eventType", EventSQSFailed),
		slog.String("jobId", jobID),
		slog.String("messageType", messageType),
		slog.Int64("durationMs", durationMs),
	}
	if err != nil {
		attrs = append(attrs, errorAttr(err))
	}
	attrs = append(attrs, extra...)
	logAttrs(ctx, slog.LevelError, "SQS message failed", attrs...)
}

// ============================================================
// ANALYSIS 이벤트 헬퍼
// ============================================================

// AnalysisStarted는 분석 작업 시작을 기록합니다.
func AnalysisStarted(ctx context.Context, jobID string, extra ...slog.Attr) {
	metrics.RecordAnalysisStarted(attrString(extra, "analysisType", "unknown"))
	attrs := []slog.Attr{
		slog.String("category", CategoryAnalysis),
		slog.String("eventType", EventAnalysisStarted),
		slog.String("jobId", jobID),
	}
	attrs = append(attrs, extra...)
	logAttrs(ctx, slog.LevelInfo, "analysis started", attrs...)
}

// AnalysisCompleted는 분석 작업 완료를 기록합니다.
func AnalysisCompleted(ctx context.Context, jobID string, durationMs int64, extra ...slog.Attr) {
	metrics.RecordAnalysisFinished(attrString(extra, "analysisType", "unknown"), "completed", durationMs)
	attrs := []slog.Attr{
		slog.String("category", CategoryAnalysis),
		slog.String("eventType", EventAnalysisCompleted),
		slog.String("jobId", jobID),
		slog.Int64("durationMs", durationMs),
	}
	attrs = append(attrs, extra...)
	logAttrs(ctx, slog.LevelInfo, "analysis completed", attrs...)
}

// AnalysisFailed는 분석 작업 실패를 기록합니다.
func AnalysisFailed(ctx context.Context, jobID string, err error, durationMs int64, extra ...slog.Attr) {
	metrics.RecordAnalysisFinished(attrString(extra, "analysisType", "unknown"), "failed", durationMs)
	attrs := []slog.Attr{
		slog.String("category", CategoryAnalysis),
		slog.String("eventType", EventAnalysisFailed),
		slog.String("jobId", jobID),
		slog.Int64("durationMs", durationMs),
	}
	if err != nil {
		attrs = append(attrs, errorAttr(err))
	}
	attrs = append(attrs, extra...)
	logAttrs(ctx, slog.LevelError, "analysis failed", attrs...)
}

// ============================================================
// JOB_STEP 타이머 (하위 단계 트래킹)
// ============================================================

// StepTimer는 단일 작업 단계의 시작/완료/실패를 기록하는 타이머입니다.
type StepTimer struct {
	ctx      context.Context
	stepName string
	jobID    string
	startAt  time.Time
}

// StepStart는 작업 단계 시작을 기록하고 완료 시점에 사용할 타이머를 반환합니다.
// 사용법:
//
//	step := logger.StepStart(ctx, "git.clone", jobID, slog.String("repo", repo))
//	if err := doWork(); err != nil { step.Fail(err); return err }
//	step.Complete()
func StepStart(ctx context.Context, stepName, jobID string, extra ...slog.Attr) *StepTimer {
	metrics.RecordStepStarted(stepName)
	attrs := []slog.Attr{
		slog.String("category", CategoryJobStep),
		slog.String("eventType", EventStepStarted),
		slog.String("jobId", jobID),
		slog.String("stepName", stepName),
	}
	attrs = append(attrs, extra...)
	logAttrs(ctx, slog.LevelInfo, stepName+" started", attrs...)
	return &StepTimer{ctx: ctx, stepName: stepName, jobID: jobID, startAt: time.Now()}
}

// Complete는 작업 단계의 정상 완료를 기록합니다.
func (s *StepTimer) Complete(extra ...slog.Attr) {
	if s == nil {
		return
	}
	durationMs := time.Since(s.startAt).Milliseconds()
	metrics.RecordStepFinished(s.stepName, "completed", durationMs)
	attrs := []slog.Attr{
		slog.String("category", CategoryJobStep),
		slog.String("eventType", EventStepCompleted),
		slog.String("jobId", s.jobID),
		slog.String("stepName", s.stepName),
		slog.Int64("durationMs", durationMs),
	}
	attrs = append(attrs, extra...)
	logAttrs(s.ctx, slog.LevelInfo, s.stepName+" completed", attrs...)
}

// Fail는 작업 단계의 실패를 기록합니다.
func (s *StepTimer) Fail(err error, extra ...slog.Attr) {
	if s == nil {
		return
	}
	durationMs := time.Since(s.startAt).Milliseconds()
	metrics.RecordStepFinished(s.stepName, "failed", durationMs)
	attrs := []slog.Attr{
		slog.String("category", CategoryJobStep),
		slog.String("eventType", EventStepFailed),
		slog.String("jobId", s.jobID),
		slog.String("stepName", s.stepName),
		slog.Int64("durationMs", durationMs),
	}
	if err != nil {
		attrs = append(attrs, errorAttr(err))
	}
	attrs = append(attrs, extra...)
	logAttrs(s.ctx, slog.LevelError, s.stepName+" failed", attrs...)
}

// ============================================================
// WORKER 이벤트 (부트/셧다운 등)
// ============================================================

func WorkerEvent(ctx context.Context, eventType, msg string, extra ...slog.Attr) {
	metrics.RecordWorkerEvent(eventType)
	attrs := []slog.Attr{
		slog.String("category", CategoryWorker),
		slog.String("eventType", eventType),
	}
	attrs = append(attrs, extra...)
	logAttrs(ctx, slog.LevelInfo, msg, attrs...)
}
