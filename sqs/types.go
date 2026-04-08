package sqs

import (
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
}

// SqsBaseMessage는 SQS 메시지 공통 래퍼
type SqsBaseMessage struct {
	TraceID string `json:"traceId"`
	JobID   string `json:"jobId"`
	Type    string `json:"type"`
	Data    any    `json:"data"`
}

