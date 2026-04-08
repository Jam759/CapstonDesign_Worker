package quest

// QuestRequest는 AI에게 보내는 퀘스트 요청
type QuestRequest struct {
	JobID                      int64             `json:"jobId"`
	Project                    Project           `json:"project"`
	Quests                     []UserQuest       `json:"quests"`
	MostRecentQuestEvaluations []QuestEvaluation `json:"mostRecentQuestEvaluations"`
}

type Project struct {
	ProjectID          int64  `json:"projectId"`
	UserID             int64  `json:"userId"`
	RepositoryFullName string `json:"repositoryFullName"`
	BranchName         string `json:"branchName"`
}

type UserQuest struct {
	UserAiQuestID   int64  `json:"userAiQuestId"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	Hint            string `json:"hint"`
	AIGenerationReason string `json:"aiGenerationReason"`
	CompletionGuide string `json:"completionGuide"`
	ApprovalStatus  string `json:"approvalStatus"`
	ProgressStatus  string `json:"progressStatus"`
	LastEvaluatedAt string `json:"lastEvaluatedAt"`
}

type QuestEvaluation struct {
	UserAiQuestID    int64   `json:"userAiQuestId"`
	EvaluationResult string  `json:"evaluationResult"`
	ConfidenceScore  float64 `json:"confidenceScore"`
	Reason           string  `json:"reason"`
	ProgressNote     string  `json:"progressNote"`
	EvaluatedAt      string  `json:"evaluatedAt,omitempty"`
}

// QuestResponse는 AI의 응답
type QuestResponse struct {
	QuestEvaluations []QuestEvaluation `json:"questEvaluations"`
	NewQuests        []NewQuest        `json:"newQuests"`
}

type NewQuest struct {
	Title              string `json:"title"`
	Description        string `json:"description"`
	Hint               string `json:"hint"`
	AIGenerationReason string `json:"aiGenerationReason"`
	CompletionGuide    string `json:"completionGuide"`
	RewardExp          int    `json:"rewardExp"`
	ExpiredAt          string `json:"expiredAt"`
}
