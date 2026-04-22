package userView

type UserView struct {
	SchemaVersion string       `json:"schemaVersion"`
	GeneratedAt   string       `json:"generatedAt"`
	Scope         Scope        `json:"scope"`
	Headline      string       `json:"headline"`
	Summary       string       `json:"summary"`
	Strengths     []string     `json:"strengths"`
	Risks         []string     `json:"risks"`
	Advice        []Advice     `json:"advice"`
	Scorecard     Scorecard    `json:"scorecard"`
	QuestSummary  QuestSummary `json:"questSummary"`
}

type Scope struct {
	ProjectID          int64  `json:"projectId"`
	UserID             int64  `json:"userId"`
	ProjectTitle       string `json:"projectTitle"`
	ProjectDescription string `json:"projectDescription"`
	ProjectGoal        string `json:"projectGoal"`
	RepositoryFullName string `json:"repositoryFullName"`
	BranchName         string `json:"branchName"`
	BeforeCommitHash   string `json:"beforeCommitHash"`
	AfterCommitHash    string `json:"afterCommitHash"`
}

type Advice struct {
	ID                string `json:"id"`
	Priority          string `json:"priority"`
	Category          string `json:"category"`
	Title             string `json:"title"`
	Body              string `json:"body"`
	RecommendedAction string `json:"recommendedAction"`
	ExpectedImpact    string `json:"expectedImpact"`
}

type Scorecard struct {
	Overall    OverallScore    `json:"overall"`
	Categories []CategoryScore `json:"categories"`
}

type OverallScore struct {
	Score      int    `json:"score"`
	Grade      string `json:"grade"`
	Confidence string `json:"confidence"`
}

type CategoryScore struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Score    int      `json:"score"`
	Reason   string   `json:"reason"`
	Evidence []string `json:"evidence"`
}

type QuestSummary struct {
	CompletedQuestIDs []int64 `json:"completedQuestIds"`
	NewQuestIDs       []int64 `json:"newQuestIds"`
}

// GenerateInput은 UserView 생성에 필요한 입력값
type GenerateInput struct {
	ProjectID          int64
	UserID             int64
	ProjectTitle       string
	ProjectDescription string
	ProjectGoal        string
	RepositoryFullName string
	BranchName         string
	BeforeCommitHash   string
	AfterCommitHash    string
	Version            int
	CompletedQuestIDs  []int64
	NewQuestIDs        []int64
}
