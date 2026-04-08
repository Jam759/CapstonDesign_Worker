package ai

import (
	"worker_GoVer/ai/strategy"
)

var ai strategy.AiStrategy = strategy.OpenAiStrategy{}

func GenerateMessage(userPrompt string, systemPrompt string) <-chan strategy.AiResult {
	return ai.GenerateMessageWithFiles(userPrompt, systemPrompt, []string{})
}

func GenerateMessageWithFiles(userPrompt string, systemPrompt string, filePaths []string) <-chan strategy.AiResult {
	return ai.GenerateMessageWithFiles(userPrompt, systemPrompt, filePaths)
}
