package strategy

import "context"

type AiResult struct {
	Data any
	Err  error
}

type AiStrategy interface {
	GenerateMessageWithFiles(ctx context.Context, userPrompt string, systemPrompt string, filePaths []string) <-chan AiResult

	GenerateMessage(ctx context.Context, userPrompt string, systemPrompt string) <-chan AiResult
}

type OpenAiStrategy struct{}
