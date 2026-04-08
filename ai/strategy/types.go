package strategy

type AiResult struct {
	Data any
	Err  error
}

type AiStrategy interface {
	GenerateMessageWithFiles(userPrompt string, systemPrompt string, filePaths []string) <-chan AiResult

	GenerateMessage(userPrompt string, systemPrompt string) <-chan AiResult
}
