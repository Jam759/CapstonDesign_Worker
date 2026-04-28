package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
	"worker_GoVer/config"
	"worker_GoVer/logger"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

var log = logger.WithComponent("ai")

func (o OpenAiStrategy) GenerateMessageWithFiles(ctx context.Context, userPrompt string, systemPrompt string, filePaths []string) <-chan AiResult {
	ch := make(chan AiResult, 1)
	go func() {
		defer close(ch)
		cfg := config.Get()
		client := openai.NewClient(option.WithAPIKey(cfg.OpenAIKey))

		if len(filePaths) == 0 {
			log.Trace(ctx, "AI chat completion start", slog.String("model", cfg.DefaultModel))
		} else {
			log.Trace(ctx, "AI assistant completion start",
				slog.String("model", cfg.DefaultModel),
				slog.Int("fileCount", len(filePaths)),
			)
		}

		var data any
		var err error
		if len(filePaths) == 0 {
			data, err = o.chatCompletion(ctx, &client, cfg, userPrompt, systemPrompt)
		} else {
			data, err = o.assistantCompletion(ctx, &client, cfg, userPrompt, systemPrompt, filePaths)
		}
		if err != nil {
			log.Error(ctx, "AI request failed", err)
		} else {
			log.Trace(ctx, "AI request completed", slog.String("model", cfg.DefaultModel))
		}
		ch <- AiResult{Data: data, Err: err}
	}()
	return ch
}

func (o OpenAiStrategy) GenerateMessage(ctx context.Context, userPrompt string, systemPrompt string) <-chan AiResult {
	return o.GenerateMessageWithFiles(ctx, userPrompt, systemPrompt, []string{})
}

// 파일 없을 때: Chat Completions API
func (o OpenAiStrategy) chatCompletion(ctx context.Context, client *openai.Client, cfg *config.Config, userPrompt, systemPrompt string) (any, error) {
	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: cfg.DefaultModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to call OpenAI API: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI API")
	}
	return resp.Choices[0].Message.Content, nil
}

// 파일 있을 때: Assistants API + code_interpreter
func (o OpenAiStrategy) assistantCompletion(ctx context.Context, client *openai.Client, cfg *config.Config, userPrompt, systemPrompt string, filePaths []string) (any, error) {
	log.Trace(ctx, "AI file upload start", slog.Int("fileCount", len(filePaths)))
	// 1. 파일 업로드 (goroutine 병렬)
	type uploadResult struct {
		fileID string
		err    error
	}
	results := make([]uploadResult, len(filePaths))
	var wg sync.WaitGroup

	for i, path := range filePaths {
		wg.Add(1)
		go func(idx int, filePath string) {
			defer wg.Done()
			f, err := os.Open(filePath)
			if err != nil {
				results[idx] = uploadResult{err: fmt.Errorf("failed to open %s: %w", filePath, err)}
				return
			}
			defer f.Close()

			uploaded, err := client.Files.New(ctx, openai.FileNewParams{
				File:    f,
				Purpose: openai.FilePurposeAssistants,
			})
			if err != nil {
				results[idx] = uploadResult{err: fmt.Errorf("failed to upload %s: %w", filePath, err)}
				return
			}
			results[idx] = uploadResult{fileID: uploaded.ID}
		}(i, path)
	}
	wg.Wait()

	// 파일 ID 수집 — 성공/실패 무관하게 모두 수집 후 defer로 삭제 보장
	var fileIDs []string
	var uploadErr error
	for _, r := range results {
		if r.err != nil {
			uploadErr = r.err
		} else {
			fileIDs = append(fileIDs, r.fileID)
		}
	}
	defer o.deleteFilesConcurrently(ctx, client, fileIDs)
	if uploadErr != nil {
		return nil, uploadErr
	}

	log.Trace(ctx, "AI files uploaded", slog.Int("fileCount", len(fileIDs)))

	// 2. Assistant 생성
	codeInterpreterTool := openai.NewCodeInterpreterToolParam()
	assistant, err := client.Beta.Assistants.New(ctx, openai.BetaAssistantNewParams{
		Model:        cfg.DefaultModel,
		Instructions: openai.String(systemPrompt),
		Tools: []openai.AssistantToolUnionParam{
			{OfCodeInterpreter: &codeInterpreterTool},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant: %w", err)
	}
	defer client.Beta.Assistants.Delete(ctx, assistant.ID)

	// 3. Attachments 구성
	var attachments []openai.BetaThreadNewParamsMessageAttachment
	for _, fid := range fileIDs {
		attachments = append(attachments, openai.BetaThreadNewParamsMessageAttachment{
			FileID: openai.String(fid),
			Tools: []openai.BetaThreadNewParamsMessageAttachmentToolUnion{
				{OfCodeInterpreter: &codeInterpreterTool},
			},
		})
	}

	// 4. Thread 생성 (메시지 + 파일 첨부 포함)
	thread, err := client.Beta.Threads.New(ctx, openai.BetaThreadNewParams{
		Messages: []openai.BetaThreadNewParamsMessage{
			{
				Role: "user",
				Content: openai.BetaThreadNewParamsMessageContentUnion{
					OfString: openai.String(userPrompt),
				},
				Attachments: attachments,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create thread: %w", err)
	}
	defer client.Beta.Threads.Delete(ctx, thread.ID)

	log.Trace(ctx, "AI assistant and thread created",
		slog.String("assistantId", assistant.ID),
		slog.String("threadId", thread.ID),
	)

	// 5. Run 실행
	run, err := client.Beta.Threads.Runs.New(ctx, thread.ID, openai.BetaThreadRunNewParams{
		AssistantID: assistant.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create run: %w", err)
	}

	log.Trace(ctx, "AI run started", slog.String("runId", run.ID))

	// 6. Polling (완료될 때까지 대기, 10초마다 로그)
	pollCount := 0
	for run.Status == openai.RunStatusQueued || run.Status == openai.RunStatusInProgress {
		time.Sleep(1 * time.Second)
		run, err = client.Beta.Threads.Runs.Get(ctx, thread.ID, run.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to poll run: %w", err)
		}
		pollCount++
		if pollCount%10 == 0 {
			log.Trace(ctx, "AI run polling",
				slog.String("runId", run.ID),
				slog.Int("elapsedSec", pollCount),
			)
		}
	}

	if run.Status != openai.RunStatusCompleted {
		return nil, fmt.Errorf("run failed with status: %s", run.Status)
	}

	// 7. 응답 가져오기
	messages, err := client.Beta.Threads.Messages.List(ctx, thread.ID, openai.BetaThreadMessageListParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	for _, msg := range messages.Data {
		if msg.Role == "assistant" {
			for _, block := range msg.Content {
				if block.Type == "text" {
					return block.Text.Value, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no response from assistant")
}

// 파일 삭제 (goroutine 병렬)
func (o OpenAiStrategy) deleteFilesConcurrently(ctx context.Context, client *openai.Client, fileIDs []string) {
	var wg sync.WaitGroup
	for _, fid := range fileIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, _ = client.Files.Delete(ctx, id)
		}(fid)
	}
	wg.Wait()
}
