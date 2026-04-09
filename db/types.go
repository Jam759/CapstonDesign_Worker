package db

import "time"

type AnalysisJob struct {
	AnalysisJobID int64     `gorm:"column:analysis_job_id;primaryKey;autoIncrement"`
	JobStatus     string    `gorm:"column:job_status;not null"`
	ProcessedAt   time.Time `gorm:"column:processed_at"`
}

func (AnalysisJob) TableName() string { return "analysis_jobs" }

type ProjectAnalysisReport struct {
	ProjectAnalysisReportsID uint      `gorm:"column:project_analysis_reports_id;primaryKey;autoIncrement"`
	Version              int       `gorm:"column:version;not null"`
	AnalysisWithReportID *int64    `gorm:"column:analysis_with_report_id"`
	CreatedAt            time.Time `gorm:"column:created_at;autoCreateTime"`
	ProjectID            int64     `gorm:"column:project_id;not null"`
	SizeBytes            *int64    `gorm:"column:size_bytes"`
	S3Bucket             string    `gorm:"column:s3_bucket"`
	BeforeCommitHash     string    `gorm:"column:before_commit_hash"`
	AfterCommitHash      string    `gorm:"column:after_commit_hash"`
	StoredURL            string    `gorm:"column:stored_url"`
	ReportType           string    `gorm:"column:report_type;not null"`
}

func (ProjectAnalysisReport) TableName() string { return "project_analysis_reports" }

type UserAiQuest struct {
	UserAiQuestID      int64      `gorm:"column:user_ai_quest_id;primaryKey;autoIncrement"`
	ProjectID          int64      `gorm:"column:project_id;not null"`
	UserID             int64      `gorm:"column:user_id;not null"`
	Title              string     `gorm:"column:title;not null"`
	Description        string     `gorm:"column:description"`
	Hint               string     `gorm:"column:hint"`
	AIGenerationReason string     `gorm:"column:ai_generation_reason"`
	CompletionGuide    string     `gorm:"column:completion_guide"`
	RewardExp          int16      `gorm:"column:reward_exp;not null"`
	ExpiredAt          time.Time  `gorm:"column:expired_at;not null"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime"`
	LastEvaluatedAt    *time.Time `gorm:"column:last_evaluated_at"`
	CompletedAt        *time.Time `gorm:"column:completed_at"`
	ApprovalStatus     string     `gorm:"column:approval_status;not null"`
	ProgressStatus     string     `gorm:"column:progress_status;not null"`
}

func (UserAiQuest) TableName() string { return "user_ai_quest" }

type UserAiQuestEvaluation struct {
	UserAiQuestEvaluationID uint      `gorm:"column:user_ai_quest_evaluation_id;primaryKey;autoIncrement"`
	UserAiQuestID           int64     `gorm:"column:user_ai_quest_id;not null"`
	AnalysisJobID           *int64    `gorm:"column:analysis_job_id"`
	EvaluationResult        string    `gorm:"column:evaluation_result;not null"`
	ConfidenceScore         float64   `gorm:"column:confidence_score"`
	Reason                  string    `gorm:"column:reason"`
	ProgressNote            string    `gorm:"column:progress_note"`
	CreatedAt               time.Time `gorm:"column:created_at;autoCreateTime"`
	EvaluatedAt             time.Time `gorm:"column:evaluated_at;not null"`
}

func (UserAiQuestEvaluation) TableName() string { return "user_ai_quest_evaluation" }
