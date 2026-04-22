package db

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type AnalysisJob struct {
	AnalysisJobID            int64     `gorm:"column:analysis_job_id;primaryKey;autoIncrement"`
	ProjectID                int64     `gorm:"column:project_id;not null"`
	UserID                   int64     `gorm:"column:user_id;not null"`
	GithubAppInstallationID  int64     `gorm:"column:github_app_installation_id;not null"`
	InstallationRepositoryID int64     `gorm:"column:installation_repository_id;not null"`
	BeforeCommitHash         string    `gorm:"column:before_commit_hash"`
	AfterCommitHash          string    `gorm:"column:after_commit_hash"`
	Branch                   string    `gorm:"column:branch;not null"`
	JobStatus                string    `gorm:"column:job_status;not null"`
	ProcessedAt              time.Time `gorm:"column:processed_at"`
	RetryCount               int16     `gorm:"column:retry_count;not null"`
	DeliveryID               string    `gorm:"column:delivery_id;not null"`
	AnalysisEventType        string    `gorm:"column:analysis_event_type;not null"`
	IsPrivateRepo            bool      `gorm:"column:is_private_repo"`
	MergeAnalysis            bool      `gorm:"column:is_merge;not null"`
}

func (AnalysisJob) TableName() string { return "analysis_jobs" }

type AnalysisJobDispatchInput struct {
	AnalysisJobID            int64  `gorm:"column:analysis_job_id"`
	ProjectID                int64  `gorm:"column:project_id"`
	UserID                   int64  `gorm:"column:user_id"`
	GithubAppInstallationID  int64  `gorm:"column:github_app_installation_id"`
	InstallationRepositoryID int64  `gorm:"column:installation_repository_id"`
	RepositoryFullName       string `gorm:"column:repository_full_name"`
	ProjectTitle             string `gorm:"column:project_title"`
	ProjectDescription       string `gorm:"column:project_description"`
	ProjectGoal              string `gorm:"column:project_goal"`
	BeforeCommitHash         string `gorm:"column:before_commit_hash"`
	AfterCommitHash          string `gorm:"column:after_commit_hash"`
	Branch                   string `gorm:"column:branch"`
	AnalysisEventType        string `gorm:"column:analysis_event_type"`
	IsPrivateRepo            DBBool `gorm:"column:is_private_repo"`
	MergeAnalysis            DBBool `gorm:"column:is_merge"`
}

type DBBool bool

func (b *DBBool) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*b = false
		return nil
	case bool:
		*b = DBBool(v)
		return nil
	case int64:
		*b = DBBool(v != 0)
		return nil
	case int:
		*b = DBBool(v != 0)
		return nil
	case uint64:
		*b = DBBool(v != 0)
		return nil
	case []byte:
		if len(v) == 1 && (v[0] == 0 || v[0] == 1) {
			*b = DBBool(v[0] != 0)
			return nil
		}
		return b.scanString(string(v))
	case string:
		return b.scanString(v)
	default:
		return fmt.Errorf("unsupported DB bool type %T", value)
	}
}

func (b DBBool) Bool() bool {
	return bool(b)
}

func (b *DBBool) scanString(value string) error {
	if len(value) == 1 && (value[0] == 0 || value[0] == 1) {
		*b = DBBool(value[0] != 0)
		return nil
	}

	normalized := strings.TrimSpace(value)
	if normalized == "" {
		*b = false
		return nil
	}

	if parsed, err := strconv.ParseBool(normalized); err == nil {
		*b = DBBool(parsed)
		return nil
	}

	n, err := strconv.ParseInt(normalized, 10, 64)
	if err != nil {
		return fmt.Errorf("unsupported DB bool value %q", value)
	}
	*b = DBBool(n != 0)
	return nil
}

type ProjectAnalysisReport struct {
	ProjectAnalysisReportsID uint      `gorm:"column:project_analysis_reports_id;primaryKey;autoIncrement"`
	Version                  int       `gorm:"column:version;not null"`
	AnalysisWithReportID     *int64    `gorm:"column:analysis_with_report_id"`
	CreatedAt                time.Time `gorm:"column:created_at;autoCreateTime"`
	ProjectID                int64     `gorm:"column:project_id;not null"`
	SizeBytes                *int64    `gorm:"column:size_bytes"`
	S3Bucket                 string    `gorm:"column:s3_bucket"`
	BeforeCommitHash         string    `gorm:"column:before_commit_hash"`
	AfterCommitHash          string    `gorm:"column:after_commit_hash"`
	StoredURL                string    `gorm:"column:stored_url"`
	ReportType               string    `gorm:"column:report_type;not null"`
}

func (ProjectAnalysisReport) TableName() string { return "project_analysis_reports" }

type UserAiQuest struct {
	UserAiQuestID      int64      `gorm:"column:user_ai_quest_id;primaryKey;autoIncrement"`
	ProjectID          int64      `gorm:"column:project_id;not null"`
	UserID             int64      `gorm:"column:user_id;not null"`
	RelatedMilestoneID *int64     `gorm:"column:related_milestone_id"`
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

type ProjectPhase struct {
	ProjectPhaseID int64     `gorm:"column:project_phase_id;primaryKey;autoIncrement"`
	ProjectID      int64     `gorm:"column:project_id;not null"`
	PhaseOrder     int       `gorm:"column:phase_order;not null"`
	PhaseName      string    `gorm:"column:phase_name;not null"`
	PhaseObjective string    `gorm:"column:phase_objective"`
	PhaseOutcome   string    `gorm:"column:phase_outcome"`
	ExitCriteria   string    `gorm:"column:exit_criteria"`
	Status         string    `gorm:"column:status;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (ProjectPhase) TableName() string { return "project_phase" }

type ProjectPhaseScope struct {
	ProjectPhaseScopeID int64     `gorm:"column:project_phase_scope_id;primaryKey;autoIncrement"`
	ProjectPhaseID      int64     `gorm:"column:project_phase_id;not null"`
	ScopeOrder          int       `gorm:"column:scope_order;not null"`
	Scope               string    `gorm:"column:scope;not null"`
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (ProjectPhaseScope) TableName() string { return "project_phase_scope" }

type ProjectMilestone struct {
	ProjectMilestoneID int64     `gorm:"column:project_milestone_id;primaryKey;autoIncrement"`
	ProjectID          int64     `gorm:"column:project_id;not null"`
	PhaseID            int64     `gorm:"column:phase_id;not null"`
	MilestoneName      string    `gorm:"column:milestone_name;not null"`
	MilestoneIntent    string    `gorm:"column:milestone_intent"`
	TriggerCondition   string    `gorm:"column:trigger_condition"`
	ExpectedState      string    `gorm:"column:expected_state"`
	CompletionRule     string    `gorm:"column:completion_rule"`
	Status             string    `gorm:"column:status;not null"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (ProjectMilestone) TableName() string { return "project_milestone" }

type ProjectMilestoneObservableEvidence struct {
	ProjectMilestoneObservableEvidenceID int64     `gorm:"column:project_milestone_observable_evidence_id;primaryKey;autoIncrement"`
	ProjectMilestoneID                   int64     `gorm:"column:project_milestone_id;not null"`
	EvidenceOrder                        int       `gorm:"column:evidence_order;not null"`
	Evidence                             string    `gorm:"column:evidence;not null"`
	CreatedAt                            time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (ProjectMilestoneObservableEvidence) TableName() string {
	return "project_milestone_observable_evidence"
}

type RoadMapPhaseInput struct {
	Key            string
	PhaseOrder     int
	PhaseName      string
	PhaseObjective string
	PhaseOutcome   string
	PhaseScopes    []string
	ExitCriteria   string
	Status         string
	Milestones     []RoadMapMilestoneInput
}

type RoadMapMilestoneInput struct {
	Key                string
	MilestoneName      string
	MilestoneIntent    string
	TriggerCondition   string
	ExpectedState      string
	ObservableEvidence []string
	CompletionRule     string
	Status             string
}

type QuestMilestoneLinkInput struct {
	UserAiQuestID int64
	MilestoneKey  string
}

type RoadMapSaveResult struct {
	PhaseIDsByKey     map[string]int64
	MilestoneIDsByKey map[string]int64
	PhaseIDs          []int64
	MilestoneIDs      []int64
	LinkedQuestIDs    []int64
	SkippedQuestLinks []QuestMilestoneLinkInput
}
