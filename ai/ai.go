package ai

import (
	"context"
	"worker_GoVer/ai/strategy"
)

var ai strategy.AiStrategy = strategy.OpenAiStrategy{}

func GenerateMessage(ctx context.Context, userPrompt string, systemPrompt string) <-chan strategy.AiResult {
	return ai.GenerateMessageWithFiles(ctx, userPrompt, systemPrompt, []string{})
}

func GenerateMessageWithFiles(ctx context.Context, userPrompt string, systemPrompt string, filePaths []string) <-chan strategy.AiResult {
	return ai.GenerateMessageWithFiles(ctx, userPrompt, systemPrompt, filePaths)
}
