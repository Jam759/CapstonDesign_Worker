package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"worker_GoVer/apperrors"
	"worker_GoVer/config"
	"worker_GoVer/logger"
	"worker_GoVer/metrics"
	"worker_GoVer/sqs/strategy"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
)

var strategyMap = map[MessageType]strategy.SqsStrategy{
	FullScanAnalysis: strategy.FullScanStrategy{},
	NormalAnalysis:   strategy.NormalAnalysisStrategy{},
}

func GetStrategy(msgType MessageType) strategy.SqsStrategy {
	return strategyMap[msgType]
}

func NewConsumer() (*Consumer, error) {
	cfg := config.Get()

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.AWSRegion),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AWSAccessKeyID, cfg.AWSSecretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, err
	}

	return &Consumer{
		client: awssqs.NewFromConfig(awsCfg),
		cfg:    cfg,
		sem:    make(chan struct{}, cfg.SQSConsumerConcurrency),
	}, nil
}

func (c *Consumer) StartAnalysisListener(ctx context.Context) {
	logger.WorkerEvent(ctx, logger.EventWorkerStarted, "analysis queue listener started")

	for {
		select {
		case <-ctx.Done():
			logger.WorkerEvent(ctx, logger.EventWorkerStopping, "analysis queue listener stopping")
			c.wg.Wait()
			logger.WorkerEvent(ctx, logger.EventWorkerStopped, "analysis queue listener stopped")
			return
		default:
		}

		output, err := c.client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.cfg.AWSAnalysisQueueURL),
			MaxNumberOfMessages: int32(c.cfg.SQSMaxNumberOfMessages),
			WaitTimeSeconds:     20, // long polling
		})
		if err != nil {
			metrics.RecordSQSPollError()
			logger.Error(ctx, "SQS receive error", err, slog.String("category", logger.CategorySQS))
			continue
		}

		for _, msg := range output.Messages {
			msg := msg
			c.sem <- struct{}{} // 슬롯 확보 (최대 동시 처리 수 제한)
			c.wg.Add(1)
			go func() {
				defer func() {
					<-c.sem
					c.wg.Done()
				}()
				c.handleAnalysisMessage(ctx, msg.Body, msg.ReceiptHandle)
			}()
		}
		// wg.Wait() 제거 — 폴링 루프는 즉시 다음 메시지를 가져옴
	}
}

func (c *Consumer) handleAnalysisMessage(ctx context.Context, body *string, receiptHandle *string) {
	if body == nil {
		return
	}

	var base SqsBaseMessage
	if err := json.Unmarshal([]byte(*body), &base); err != nil {
		logger.Error(ctx, "failed to unmarshal SQS base message", err, slog.String("category", logger.CategorySQS))
		return
	}

	msgCtx := logger.WithTraceID(ctx, base.TraceID)
	msgCtx = logger.WithJobID(msgCtx, base.JobID)
	startAt := time.Now()

	logger.SQSReceived(msgCtx, base.JobID, base.Type)

	s := GetStrategy(MessageType(base.Type))
	if s == nil {
		logger.Warn(msgCtx, "unknown SQS message type", slog.String("messageType", base.Type))
		return
	}

	dataBytes, err := json.Marshal(base.Data)
	if err != nil {
		logger.Error(msgCtx, "failed to re-marshal SQS payload", err)
		return
	}

	// 처리 중 visibility timeout 주기적 연장 (분석 작업이 길어져도 중복 처리 방지)
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	go func() {
		ticker := time.NewTicker(4 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.resetMessageVisibility(msgCtx, c.cfg.AWSAnalysisQueueURL, receiptHandle); err != nil {
					logger.Warn(msgCtx, "failed to extend SQS visibility timeout",
						slog.String("reason", err.Error()),
					)
				}
			case <-heartbeatCtx.Done():
				return
			}
		}
	}()

	result, err := s.Handle(msgCtx, base.JobID, dataBytes)
	if err != nil {
		durationMs := time.Since(startAt).Milliseconds()
		logger.SQSFailed(msgCtx, base.JobID, base.Type, err, durationMs)

		var analysisErr *apperrors.AnalysisError
		if !errors.As(err, &analysisErr) {
			// 예상치 못한 에러 → 범용 AnalysisError로 래핑
			analysisErr = apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "unexpected error")
		}
		// 모든 에러: 메시지 삭제 + FAILED 알림 (알림큐에서 retryable 판단)
		if delErr := c.deleteMessage(msgCtx, c.cfg.AWSAnalysisQueueURL, receiptHandle); delErr != nil {
			logger.Warn(msgCtx, "failed to delete SQS message after failure",
				slog.String("reason", delErr.Error()),
			)
		}
		c.publishFailNotification(msgCtx, base, analysisErr)
		return
	}

	if err := c.deleteMessage(msgCtx, c.cfg.AWSAnalysisQueueURL, receiptHandle); err != nil {
		logger.Warn(msgCtx, "failed to delete SQS message after success",
			slog.String("reason", err.Error()),
		)
	}

	if result != nil {
		successData := SuccessMessage{
			CompleteQuestIDs: result.CompleteQuestIDs,
			NewQuestIDs:      result.NewQuestIDs,
			NewProjectKBID:   result.NewProjectKBID,
			UserViewReportID: result.UserViewReportID,
		}
		notification := NotificationQueueBaseMessage{
			TraceID:   base.TraceID,
			JobID:     base.JobID,
			EventType: AnalysisEventType(base.Type),
			Status:    StatusSuccess,
			Data:      successData,
		}
		if err := c.PublishNotification(msgCtx, notification); err != nil {
			logger.Error(msgCtx, "failed to publish success notification", err)
		}
	}

	logger.SQSProcessed(msgCtx, base.JobID, base.Type, time.Since(startAt).Milliseconds())
}

func (c *Consumer) publishFailNotification(ctx context.Context, base SqsBaseMessage, ae *apperrors.AnalysisError) {
	failData := FailMessage{
		ErrorCode:    string(ae.Code),
		ErrorMessage: ae.Error(),
		HTTPStatus:   ae.HTTPStatus,
		Retryable:    ae.Retryable,
	}
	notification := NotificationQueueBaseMessage{
		TraceID:   base.TraceID,
		JobID:     base.JobID,
		EventType: AnalysisEventType(base.Type),
		Status:    StatusFailed,
		Data:      failData,
	}
	if err := c.PublishNotification(ctx, notification); err != nil {
		logger.Error(ctx, "failed to publish failure notification", err)
	}
}

// PublishNotification은 알림 큐에 메시지를 발행합니다.
func (c *Consumer) PublishNotification(ctx context.Context, msg NotificationQueueBaseMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal notification message: %w", err)
	}

	_, err = c.client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl:    aws.String(c.cfg.AWSNotificationQueueURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		metrics.RecordNotificationPublished(string(msg.EventType), "failed")
		return fmt.Errorf("failed to publish notification: %w", err)
	}

	metrics.RecordNotificationPublished(string(msg.EventType), "published")

	logger.Info(ctx, "SQS notification published",
		slog.String("jobId", msg.JobID),
		slog.String("status", string(msg.Status)),
		slog.String("eventType", string(msg.EventType)),
	)
	return nil
}

func (c *Consumer) deleteMessage(ctx context.Context, queueURL string, receiptHandle *string) error {
	_, err := c.client.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueURL),
		ReceiptHandle: receiptHandle,
	})
	return err
}

// resetMessageVisibility는 메시지의 visibility timeout을 5분 연장합니다.
// 장시간 처리 중 SQS가 메시지를 재발행하여 중복 처리되는 것을 방지합니다.
func (c *Consumer) resetMessageVisibility(ctx context.Context, queueURL string, receiptHandle *string) error {
	_, err := c.client.ChangeMessageVisibility(ctx, &awssqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(queueURL),
		ReceiptHandle:     receiptHandle,
		VisibilityTimeout: 300, // 5분
	})
	return err
}
