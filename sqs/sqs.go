package sqs

import (
	"context"
	"encoding/json"
	"log"
	"sync"
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

		var wg sync.WaitGroup
		for _, msg := range output.Messages {
			msg := msg
			c.sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer func() { <-c.sem; wg.Done() }()
				c.handleAnalysisMessage(ctx, msg.Body, msg.ReceiptHandle)
			}()
		}
		wg.Wait()
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

	if err := s.Handle(ctx, base.JobID, dataBytes); err != nil {
		log.Printf("[SQS] strategy handle error jobId=%s type=%s: %v", base.JobID, base.Type, err)
		if visErr := c.resetMessageVisibility(ctx, c.cfg.AWSAnalysisQueueURL, receiptHandle); visErr != nil {
			log.Printf("[SQS] failed to reset visibility jobId=%s: %v", base.JobID, visErr)
		} else {
			log.Printf("[SQS] message re-queued jobId=%s", base.JobID)
		}
		return
	}

	if err := c.deleteMessage(ctx, c.cfg.AWSAnalysisQueueURL, receiptHandle); err != nil {
		log.Printf("[SQS] failed to delete message jobId=%s: %v", base.JobID, err)
	}
}

func (c *Consumer) deleteMessage(ctx context.Context, queueURL string, receiptHandle *string) error {
	_, err := c.client.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueURL),
		ReceiptHandle: receiptHandle,
	})
	return err
}

// resetMessageVisibility는 메시지를 30초 후 재처리되도록 visibility timeout을 설정합니다.
func (c *Consumer) resetMessageVisibility(ctx context.Context, queueURL string, receiptHandle *string) error {
	_, err := c.client.ChangeMessageVisibility(ctx, &awssqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(queueURL),
		ReceiptHandle:     receiptHandle,
		VisibilityTimeout: 30,
	})
	return err
}
