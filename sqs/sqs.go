package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"worker_GoVer/apperrors"
	"worker_GoVer/config"
	"worker_GoVer/db"
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
			WaitTimeSeconds:     20,
		})
		if err != nil {
			metrics.RecordSQSPollError()
			logger.Error(ctx, "SQS receive error", err, slog.String("category", logger.CategorySQS))
			continue
		}

		for _, msg := range output.Messages {
			msg := msg
			c.sem <- struct{}{}
			c.wg.Add(1)
			go func() {
				defer func() {
					<-c.sem
					c.wg.Done()
				}()
				c.handleAnalysisMessage(ctx, msg.Body, msg.ReceiptHandle)
			}()
		}
	}
}

func (c *Consumer) handleAnalysisMessage(ctx context.Context, body *string, receiptHandle *string) {
	if body == nil {
		return
	}

	var incoming AnalysisQueueMessage
	if err := json.Unmarshal([]byte(*body), &incoming); err != nil {
		logger.Error(ctx, "failed to unmarshal analysis queue message", err, slog.String("category", logger.CategorySQS))
		c.discardAnalysisMessage(ctx, receiptHandle, "discarding malformed analysis queue message")
		return
	}

	msgCtx := logger.WithTraceID(ctx, incoming.TraceID)
	incomingJobID := strings.TrimSpace(incoming.JobID)
	if incomingJobID == "" {
		logger.Warn(msgCtx, "analysis queue message missing jobId")
		c.discardAnalysisMessage(msgCtx, receiptHandle, "discarding analysis queue message without jobId")
		return
	}
	msgCtx = logger.WithJobID(msgCtx, incomingJobID)

	jobIDInt, err := strconv.ParseInt(incomingJobID, 10, 64)
	if err != nil {
		logger.Warn(msgCtx, "invalid job id in analysis queue message", slog.String("reason", err.Error()))
		c.discardAnalysisMessage(msgCtx, receiptHandle, "discarding analysis queue message with invalid jobId")
		return
	}

	jobInput, err := db.GetAnalysisJobDispatchInput(jobIDInt)
	if err != nil {
		logger.Error(msgCtx, "failed to load analysis job", err, slog.String("category", logger.CategoryAnalysis))
		return
	}
	if jobInput == nil {
		logger.Warn(msgCtx, "analysis job not found for queue message", slog.Int64("jobId", jobIDInt))
		c.discardAnalysisMessage(msgCtx, receiptHandle, "discarding analysis queue message for missing job")
		return
	}

	base, err := buildStrategyMessage(jobInput, logger.TraceIDFromContext(msgCtx))
	if err != nil {
		logger.Error(msgCtx, "invalid analysis job input", err, slog.String("category", logger.CategorySQS))
		if delErr := c.deleteMessage(msgCtx, c.cfg.AWSAnalysisQueueURL, receiptHandle); delErr != nil {
			logger.Warn(msgCtx, "failed to delete invalid SQS message",
				slog.String("reason", delErr.Error()),
			)
		}

		var analysisErr *apperrors.AnalysisError
		if !errors.As(err, &analysisErr) {
			analysisErr = apperrors.Newf(apperrors.ErrInvalidJobData, 422, false, err, "invalid analysis job input")
		}
		if err := c.publishFailNotification(msgCtx, jobIDInt, base, analysisErr); err != nil {
			logger.Error(msgCtx, "failed to publish failure notification", err)
			c.updateAnalysisJobStatus(msgCtx, jobIDInt, "FAILED")
		}
		return
	}

	msgCtx = logger.WithTraceID(ctx, base.TraceID)
	msgCtx = logger.WithJobID(msgCtx, base.JobID)
	startAt := time.Now()

	logger.SQSReceived(msgCtx, base.JobID, base.Type)

	s := GetStrategy(MessageType(base.Type))
	if s == nil {
		analysisErr := apperrors.Newf(
			apperrors.ErrInvalidJobData,
			422,
			false,
			nil,
			"jobId=%s unsupported analysis event type=%s",
			base.JobID,
			base.Type,
		)
		logger.Warn(msgCtx, "unknown analysis event type", slog.String("messageType", base.Type))
		if delErr := c.deleteMessage(msgCtx, c.cfg.AWSAnalysisQueueURL, receiptHandle); delErr != nil {
			logger.Warn(msgCtx, "failed to delete SQS message with unknown type",
				slog.String("reason", delErr.Error()),
			)
		}
		if err := c.publishFailNotification(msgCtx, jobIDInt, base, analysisErr); err != nil {
			logger.Error(msgCtx, "failed to publish failure notification", err)
			c.updateAnalysisJobStatus(msgCtx, jobIDInt, "FAILED")
		}
		return
	}

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

	result, err := s.Handle(msgCtx, base)
	if err != nil {
		durationMs := time.Since(startAt).Milliseconds()
		logger.SQSFailed(msgCtx, base.JobID, base.Type, err, durationMs)

		var analysisErr *apperrors.AnalysisError
		if !errors.As(err, &analysisErr) {
			analysisErr = apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "unexpected error")
		}

		if delErr := c.deleteMessage(msgCtx, c.cfg.AWSAnalysisQueueURL, receiptHandle); delErr != nil {
			logger.Warn(msgCtx, "failed to delete SQS message after failure",
				slog.String("reason", delErr.Error()),
			)
		}
		if err := c.publishFailNotification(msgCtx, jobIDInt, base, analysisErr); err != nil {
			logger.Error(msgCtx, "failed to publish failure notification", err)
			c.updateAnalysisJobStatus(msgCtx, jobIDInt, "FAILED")
		}
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
			JobID:     jobIDInt,
			UserID:    base.UserID,
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

func buildStrategyMessage(job *db.AnalysisJobDispatchInput, traceID string) (strategy.SqsBaseMessage, error) {
	base := strategy.SqsBaseMessage{
		TraceID: traceID,
		JobID:   strconv.FormatInt(job.AnalysisJobID, 10),
		UserID:  job.UserID,
		Type:    job.AnalysisEventType,
	}

	if err := validateCommonJobInput(job); err != nil {
		return base, err
	}

	switch MessageType(job.AnalysisEventType) {
	case FullScanAnalysis:
		base.Data = strategy.FullScanQueueMessage{
			RepositoryFullName: job.RepositoryFullName,
			BranchName:         job.Branch,
			RepositoryID:       job.InstallationRepositoryID,
			InstallationID:     job.GithubAppInstallationID,
			IsPrivate:          job.IsPrivateRepo.Bool(),
			ProjectID:          job.ProjectID,
			ProjectTitle:       job.ProjectTitle,
			ProjectDescription: job.ProjectDescription,
			ProjectGoal:        job.ProjectGoal,
		}
		return base, nil
	case NormalAnalysis:
		if err := requireNotBlank(job.AnalysisJobID, "before_commit_hash", job.BeforeCommitHash); err != nil {
			return base, err
		}
		if err := requireNotBlank(job.AnalysisJobID, "after_commit_hash", job.AfterCommitHash); err != nil {
			return base, err
		}
		base.Data = strategy.NormalAnalysisQueueMessage{
			PushUserInstallationID: job.GithubAppInstallationID,
			RepositoryID:           job.InstallationRepositoryID,
			RepositoryFullName:     job.RepositoryFullName,
			BeforeCommit:           job.BeforeCommitHash,
			AfterCommit:            job.AfterCommitHash,
			BranchName:             job.Branch,
			IsPrivate:              job.IsPrivateRepo.Bool(),
			ProjectID:              job.ProjectID,
			ProjectTitle:           job.ProjectTitle,
			ProjectDescription:     job.ProjectDescription,
			ProjectGoal:            job.ProjectGoal,
			IsMerge:                job.MergeAnalysis.Bool(),
		}
		return base, nil
	default:
		return base, apperrors.Newf(
			apperrors.ErrInvalidJobData,
			422,
			false,
			nil,
			"jobId=%d unsupported analysis event type=%s",
			job.AnalysisJobID,
			job.AnalysisEventType,
		)
	}
}

func validateCommonJobInput(job *db.AnalysisJobDispatchInput) error {
	if err := requirePositiveInt64(job.AnalysisJobID, "user_id", job.UserID); err != nil {
		return err
	}
	if err := requirePositiveInt64(job.AnalysisJobID, "project_id", job.ProjectID); err != nil {
		return err
	}
	if err := requirePositiveInt64(job.AnalysisJobID, "github_app_installation_id", job.GithubAppInstallationID); err != nil {
		return err
	}
	if err := requirePositiveInt64(job.AnalysisJobID, "installation_repository_id", job.InstallationRepositoryID); err != nil {
		return err
	}
	if err := requireNotBlank(job.AnalysisJobID, "branch", job.Branch); err != nil {
		return err
	}
	if err := requireNotBlank(job.AnalysisJobID, "repository_full_name", job.RepositoryFullName); err != nil {
		return err
	}
	return nil
}

func requirePositiveInt64(jobID int64, field string, value int64) error {
	if value > 0 {
		return nil
	}
	return apperrors.Newf(
		apperrors.ErrInvalidJobData,
		422,
		false,
		nil,
		"jobId=%d missing or invalid %s",
		jobID,
		field,
	)
}

func requireNotBlank(jobID int64, field string, value string) error {
	if strings.TrimSpace(value) != "" {
		return nil
	}
	return apperrors.Newf(
		apperrors.ErrInvalidJobData,
		422,
		false,
		nil,
		"jobId=%d missing %s",
		jobID,
		field,
	)
}

func (c *Consumer) publishFailNotification(ctx context.Context, jobID int64, base strategy.SqsBaseMessage, ae *apperrors.AnalysisError) error {
	failData := FailMessage{
		ErrorCode:    string(ae.Code),
		ErrorMessage: ae.Error(),
		HTTPStatus:   ae.HTTPStatus,
		Retryable:    ae.Retryable,
	}
	notification := NotificationQueueBaseMessage{
		TraceID:   base.TraceID,
		JobID:     jobID,
		UserID:    base.UserID,
		EventType: AnalysisEventType(base.Type),
		Status:    StatusFailed,
		Data:      failData,
	}
	return c.PublishNotification(ctx, notification)
}

func (c *Consumer) updateAnalysisJobStatus(ctx context.Context, jobID int64, status string) {
	if err := db.UpdateAnalysisJobStatus(jobID, status); err != nil {
		logger.Warn(ctx, "failed to update analysis job status",
			slog.Int64("jobID", jobID),
			slog.String("status", status),
			slog.String("reason", err.Error()),
		)
	}
}

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
		slog.Int64("jobId", msg.JobID),
		slog.String("status", string(msg.Status)),
		slog.String("eventType", string(msg.EventType)),
	)
	return nil
}

func (c *Consumer) discardAnalysisMessage(ctx context.Context, receiptHandle *string, reason string) {
	if err := c.deleteMessage(ctx, c.cfg.AWSAnalysisQueueURL, receiptHandle); err != nil {
		logger.Warn(ctx, "failed to discard invalid analysis queue message",
			slog.String("reason", err.Error()),
		)
		return
	}
	logger.Warn(ctx, reason)
}

func (c *Consumer) deleteMessage(ctx context.Context, queueURL string, receiptHandle *string) error {
	_, err := c.client.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueURL),
		ReceiptHandle: receiptHandle,
	})
	return err
}

func (c *Consumer) resetMessageVisibility(ctx context.Context, queueURL string, receiptHandle *string) error {
	_, err := c.client.ChangeMessageVisibility(ctx, &awssqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(queueURL),
		ReceiptHandle:     receiptHandle,
		VisibilityTimeout: 300,
	})
	return err
}
