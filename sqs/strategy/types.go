package strategy

import (
	"context"
	"encoding/json"
)

// StrategyResult는 strategy 성공 시 알림 큐에 전달할 결과값
type StrategyResult struct {
	CompleteQuestIDs []int64
	NewQuestIDs      []int64
	NewProjectKBID   *int64
	UserViewReportID *int64
}

type SqsStrategy interface {
	Handle(ctx context.Context, jobID string, data json.RawMessage) (*StrategyResult, error)
}

type FullScanStrategy struct{}

type NormalAnalysisStrategy struct{}

// FullScanQueueMessage는 전체 분석 큐 메시지 데이터
type FullScanQueueMessage struct {
	RepositoryFullName string `json:"repositoryFullName"`
	BranchName         string `json:"branchName"`
	RepositoryID       int64  `json:"repositoryId"`
	InstallationID     int64  `json:"installationId"`
	IsPrivate          bool   `json:"isPrivate"`
	ProjectID          int64  `json:"projectId"`
	UserID             int64  `json:"userId"`
	JobID              int64  `json:"jobId"`
}

// NormalAnalysisQueueMessage는 일반 분석 큐 메시지 데이터
type NormalAnalysisQueueMessage struct {
	PushUserInstallationID int64  `json:"pushUserInstallationId"`
	RepositoryID           int64  `json:"repositoryId"`
	RepositoryFullName     string `json:"repositoryFullName"`
	BeforeCommit           string `json:"beforeCommit"`
	AfterCommit            string `json:"afterCommit"`
	BranchName             string `json:"branchName"`
	IsPrivate              bool   `json:"isPrivate"`
	ProjectID              int64  `json:"projectId"`
	PushUserID             int64  `json:"pushUserId"`
	IsMerge                bool   `json:"isMerge"`
	JobID                  int64  `json:"jobId"`
}
