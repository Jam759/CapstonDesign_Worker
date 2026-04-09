package logger

import (
	"context"

	"github.com/google/uuid"
)

type ctxKey string

const (
	ctxKeyTraceID ctxKey = "traceId"
	ctxKeyJobID   ctxKey = "jobId"
)

// WithTraceID는 context에 traceId를 주입합니다. 빈 문자열이면 새 UUID를 생성합니다.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		traceID = uuid.NewString()
	}
	return context.WithValue(ctx, ctxKeyTraceID, traceID)
}

// TraceIDFromContext는 context에서 traceId를 꺼냅니다. 없으면 빈 문자열.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKeyTraceID).(string); ok {
		return v
	}
	return ""
}

// WithJobID는 context에 jobId를 주입합니다.
func WithJobID(ctx context.Context, jobID string) context.Context {
	return context.WithValue(ctx, ctxKeyJobID, jobID)
}

// JobIDFromContext는 context에서 jobId를 꺼냅니다. 없으면 빈 문자열.
func JobIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKeyJobID).(string); ok {
		return v
	}
	return ""
}
