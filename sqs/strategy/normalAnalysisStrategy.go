package strategy

import (
	"context"
	"encoding/json"
	"log"
)

type NormalAnalysisStrategy struct{}

func (s NormalAnalysisStrategy) Handle(ctx context.Context, jobID string, data json.RawMessage) error {
	var msg NormalAnalysisQueueMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}

	log.Printf("[NormalAnalysis] jobId=%s repo=%s branch=%s isMerge=%v",
		jobID, msg.RepositoryFullName, msg.BranchName, msg.IsMerge)

	// TODO: 일반 분석 처리 로직
	return nil
}
