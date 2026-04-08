package db

import (
	"fmt"
	"time"
	"worker_GoVer/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var conn *gorm.DB

type ProjectAnalysisReport struct {
	ProjectMetaReportsID uint      `gorm:"column:project_meta_reports_id;primaryKey;autoIncrement"`
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

func Init() error {
	cfg := config.Get()

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=Asia%%2FSeoul",
		cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName,
	)

	logLevel := logger.Silent
	if cfg.Debug {
		logLevel = logger.Info
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return fmt.Errorf("failed to open DB: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DBMaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.DBConnMaxLifetimeMin) * time.Minute)

	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping DB: %w", err)
	}

	conn = db
	return nil
}

func NextReportVersion(projectID int64, reportType string) (int, error) {
	var version int
	err := conn.Model(&ProjectAnalysisReport{}).
		Where("project_id = ? AND report_type = ?", projectID, reportType).
		Select("COALESCE(MAX(version), 0) + 1").
		Scan(&version).Error
	if err != nil {
		return 0, fmt.Errorf("failed to query version: %w", err)
	}
	return version, nil
}

func InsertAnalysisReport(
	projectID int64,
	analysisJobID int64,
	reportType string,
	version int,
	s3Bucket string,
	storedURL string,
	sizeBytes int64,
	beforeCommitHash string,
	afterCommitHash string,
) error {
	record := ProjectAnalysisReport{
		ProjectID:            projectID,
		AnalysisWithReportID: &analysisJobID,
		ReportType:           reportType,
		Version:              version,
		S3Bucket:             s3Bucket,
		StoredURL:            storedURL,
		SizeBytes:            &sizeBytes,
		BeforeCommitHash:     beforeCommitHash,
		AfterCommitHash:      afterCommitHash,
	}
	if err := conn.Create(&record).Error; err != nil {
		return fmt.Errorf("failed to insert %s report: %w", reportType, err)
	}
	return nil
}

func InsertQuest(
	projectID int64,
	userID int64,
	title string,
	description string,
	hint string,
	aiGenerationReason string,
	completionGuide string,
	rewardExp int,
	expiredAt string,
) (int64, error) {
	t, err := time.ParseInLocation("2006-01-02T15:04:05", expiredAt, time.Local)
	if err != nil {
		return 0, fmt.Errorf("failed to parse expiredAt: %w", err)
	}
	record := UserAiQuest{
		ProjectID:          projectID,
		UserID:             userID,
		Title:              title,
		Description:        description,
		Hint:               hint,
		AIGenerationReason: aiGenerationReason,
		CompletionGuide:    completionGuide,
		RewardExp:          int16(rewardExp),
		ExpiredAt:          t,
		ApprovalStatus:     "REQUEST_PENDING",
		ProgressStatus:     "ACTIVE",
	}
	if err := conn.Create(&record).Error; err != nil {
		return 0, fmt.Errorf("failed to insert quest: %w", err)
	}
	return record.UserAiQuestID, nil
}

func InsertQuestEvaluation(
	questID int64,
	analysisJobID int64,
	evaluationResult string,
	confidenceScore float64,
	reason string,
	progressNote string,
) error {
	now := time.Now()
	record := UserAiQuestEvaluation{
		UserAiQuestID:    questID,
		AnalysisJobID:    &analysisJobID,
		EvaluationResult: evaluationResult,
		ConfidenceScore:  confidenceScore,
		Reason:           reason,
		ProgressNote:     progressNote,
		EvaluatedAt:      now,
	}
	if err := conn.Create(&record).Error; err != nil {
		return fmt.Errorf("failed to insert quest evaluation: %w", err)
	}
	return nil
}

func CompleteQuest(questID int64) error {
	now := time.Now()
	err := conn.Model(&UserAiQuest{}).
		Where("user_ai_quest_id = ?", questID).
		Updates(map[string]any{
			"progress_status":   "COMPLETED",
			"approval_status":   "CLEARED",
			"completed_at":      now,
			"last_evaluated_at": now,
		}).Error
	if err != nil {
		return fmt.Errorf("failed to complete quest: %w", err)
	}
	return nil
}

// FetchActiveQuests는 project_id + user_id 기준 ACTIVE 및 ACCEPT 상태의 퀘스트를 조회합니다.
func FetchActiveQuests(projectID int64, userID int64) ([]UserAiQuest, error) {
	var quests []UserAiQuest
	err := conn.Where("project_id = ? AND user_id = ? AND progress_status = 'ACTIVE' AND approval_status = 'REQUEST_ACCEPT'", projectID, userID).
		Find(&quests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to fetch active quests: %w", err)
	}
	return quests, nil
}

// FetchRecentEvaluations는 퀘스트 ID 목록에 대한 가장 최근 평가를 조회합니다.
func FetchRecentEvaluations(questIDs []int64) ([]UserAiQuestEvaluation, error) {
	if len(questIDs) == 0 {
		return nil, nil
	}
	var evals []UserAiQuestEvaluation
	err := conn.Where("user_ai_quest_id IN ? AND evaluated_at IN (?)",
		questIDs,
		conn.Model(&UserAiQuestEvaluation{}).
			Select("MAX(evaluated_at)").
			Where("user_ai_quest_id IN ?", questIDs).
			Group("user_ai_quest_id"),
	).Find(&evals).Error
	if err != nil {
		return nil, fmt.Errorf("failed to fetch recent evaluations: %w", err)
	}
	return evals, nil
}

func UpdateQuestLastEvaluatedAt(questID int64) error {
	err := conn.Model(&UserAiQuest{}).
		Where("user_ai_quest_id = ?", questID).
		Update("last_evaluated_at", time.Now()).Error
	if err != nil {
		return fmt.Errorf("failed to update quest last_evaluated_at: %w", err)
	}
	return nil
}
