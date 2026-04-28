package strategy

import (
	"context"
	"log/slog"
	"worker_GoVer/logger"
)

var log = logger.WithComponent("sqs")

// rollbackList는 분석 실패 시 순서 역순으로 실행할 정리 작업을 모읍니다.
type rollbackList struct {
	actions []func()
}

func (r *rollbackList) Add(fn func()) {
	r.actions = append(r.actions, fn)
}

// Run은 등록된 롤백 작업을 역순으로 실행합니다.
func (r *rollbackList) Run(ctx context.Context) {
	if len(r.actions) == 0 {
		return
	}
	log.Warn(ctx, "analysis rollback started", nil, slog.Int("stepCount", len(r.actions)))
	for i := len(r.actions) - 1; i >= 0; i-- {
		r.actions[i]()
	}
	log.Trace(ctx, "analysis rollback completed")
}
