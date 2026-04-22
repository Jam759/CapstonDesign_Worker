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
	wg     sync.WaitGroup
}

// AnalysisQueueMessage is the producer contract from the main server.
type AnalysisQueueMessage struct {
	TraceID string `json:"traceId"`
	JobID   string `json:"jobId"`
}

type AnalysisEventType string

const (
	EventFullScan       AnalysisEventType = "FULL_SCAN_ANALYSIS_REQUEST"
	EventNormalAnalysis AnalysisEventType = "NORMAL_ANALYSIS_REQUEST"
)

type AnalysisStatus string

const (
	StatusSuccess AnalysisStatus = "SUCCESS"
	StatusFailed  AnalysisStatus = "FAILED"
)

type NotificationQueueBaseMessage struct {
	TraceID   string            `json:"traceId"`
	JobID     int64             `json:"jobId"`
	UserID    int64             `json:"userId"`
	EventType AnalysisEventType `json:"eventType"`
	Status    AnalysisStatus    `json:"status"`
	Data      any               `json:"data"`
}

type SuccessMessage struct {
	CompleteQuestIDs []int64 `json:"completeQuestIds"`
	NewQuestIDs      []int64 `json:"newQuestIds"`
	NewProjectKBID   *int64  `json:"newProjectKBid"`
	UserViewReportID *int64  `json:"userViewReportId"`
}

type FailMessage struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	HTTPStatus   int    `json:"HTTPStatus"`
	Retryable    bool   `json:"retryable"`
}
