package sqs

import (
	"sync"
	"worker_GoVer/config"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type MessageType string

const (
	FullScanAnalysis MessageType = "FULL_SCAN_ANALYSIS_REQUEST"
	NormalAnalysis   MessageType = "NORMAL_ANALYSIS_REQUEST"
)

type Consumer struct {
	client *sqs.Client
	cfg    *config.Config
	sem    chan struct{}
	wg     sync.WaitGroup // 진행 중인 모든 작업 추적 (graceful shutdown용)
}

// SqsBaseMessage는 SQS 메시지 공통 래퍼
type SqsBaseMessage struct {
	TraceID string `json:"traceId"`
	JobID   string `json:"jobId"`
	Type    string `json:"type"`
	Data    any    `json:"data"`
}

// AnalysisEventType은 분석 이벤트 타입
type AnalysisEventType string

const (
	EventFullScan      AnalysisEventType = "FULL_SCAN_ANALYSIS_REQUEST"
	EventNormalAnalysis AnalysisEventType = "NORMAL_ANALYSIS_REQUEST"
)

// AnalysisStatus는 분석 작업 결과 상태
type AnalysisStatus string

const (
	StatusSuccess AnalysisStatus = "SUCCESS"
	StatusFailed  AnalysisStatus = "FAILED"
)

// NotificationQueueBaseMessage는 알림 큐에 발행하는 메시지 래퍼
type NotificationQueueBaseMessage struct {
	TraceID   string            `json:"traceId"`
	JobID     string            `json:"jobId"`
	EventType AnalysisEventType `json:"eventType"`
	Status    AnalysisStatus    `json:"status"`
	Data      any               `json:"data"`
}

// SuccessMessage는 분석 성공 시 알림 큐에 담기는 데이터
type SuccessMessage struct {
	CompleteQuestIDs []int64 `json:"completeQuestIds"`
	NewQuestIDs      []int64 `json:"newQuestIds"`
	NewProjectKBID   *int64  `json:"newProjectKBid"`
	UserViewReportID *int64  `json:"userViewReportId"`
}

// FailMessage는 분석 실패 시 알림 큐에 담기는 데이터
type FailMessage struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	HTTPStatus   int    `json:"HTTPStatus"`
	Retryable    bool   `json:"retryable"`
}

