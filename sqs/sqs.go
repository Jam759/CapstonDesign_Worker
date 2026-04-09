package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"
	"worker_GoVer/apperrors"
	"worker_GoVer/config"
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
	log.Println("[SQS] analysis queue listener started")

	for {
		select {
		case <-ctx.Done():
			log.Println("[SQS] shutdown: waiting for in-flight jobs...")
			c.wg.Wait()
			log.Println("[SQS] analysis queue listener stopped")
			return
		default:
		}

		output, err := c.client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.cfg.AWSAnalysisQueueURL),
			MaxNumberOfMessages: int32(c.cfg.SQSMaxNumberOfMessages),
			WaitTimeSeconds:     20, // long polling
		})
		if err != nil {
			log.Printf("[SQS] receive error: %v", err)
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
		log.Printf("[SQS] failed to unmarshal base message: %v", err)
		return
	}

	log.Printf("[SQS] received jobId=%s type=%s traceId=%s", base.JobID, base.Type, base.TraceID)

	s := GetStrategy(MessageType(base.Type))
	if s == nil {
		log.Printf("[SQS] unknown message type: %s", base.Type)
		return
	}

	dataBytes, err := json.Marshal(base.Data)
	if err != nil {
		log.Printf("[SQS] failed to re-marshal data: %v", err)
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
				if err := c.resetMessageVisibility(ctx, c.cfg.AWSAnalysisQueueURL, receiptHandle); err != nil {
					log.Printf("[SQS] failed to extend visibility timeout jobId=%s: %v", base.JobID, err)
				}
			case <-heartbeatCtx.Done():
				return
			}
		}
	}()

	result, err := s.Handle(ctx, base.JobID, dataBytes)
	if err != nil {
		log.Printf("[SQS] strategy handle error jobId=%s type=%s: %v", base.JobID, base.Type, err)

		var analysisErr *apperrors.AnalysisError
		if !errors.As(err, &analysisErr) {
			// 예상치 못한 에러 → 범용 AnalysisError로 래핑
			analysisErr = apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "unexpected error")
		}
		// 모든 에러: 메시지 삭제 + FAILED 알림 (알림큐에서 retryable 판단)
		if delErr := c.deleteMessage(ctx, c.cfg.AWSAnalysisQueueURL, receiptHandle); delErr != nil {
			log.Printf("[SQS] failed to delete message jobId=%s: %v", base.JobID, delErr)
		}
		c.publishFailNotification(ctx, base, analysisErr)
		return
	}

	if err := c.deleteMessage(ctx, c.cfg.AWSAnalysisQueueURL, receiptHandle); err != nil {
		log.Printf("[SQS] failed to delete message jobId=%s: %v", base.JobID, err)
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
		if err := c.PublishNotification(ctx, notification); err != nil {
			log.Printf("[SQS] failed to publish success notification jobId=%s: %v", base.JobID, err)
		}
	}
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
		log.Printf("[SQS] failed to publish fail notification jobId=%s: %v", base.JobID, err)
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
		return fmt.Errorf("failed to publish notification: %w", err)
	}

	log.Printf("[SQS] notification published jobId=%s status=%s", msg.JobID, msg.Status)
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
