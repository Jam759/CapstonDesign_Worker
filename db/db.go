package db

import (
	"errors"
	"fmt"
	"time"
	"worker_GoVer/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var conn *gorm.DB

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
	relatedReportID *int64,
	reportType string,
	version int,
	s3Bucket string,
	storedURL string,
	sizeBytes int64,
	beforeCommitHash string,
	afterCommitHash string,
) (int64, error) {
	record := ProjectAnalysisReport{
		ProjectID:            projectID,
		AnalysisWithReportID: relatedReportID,
		ReportType:           reportType,
		Version:              version,
		S3Bucket:             s3Bucket,
		StoredURL:            storedURL,
		SizeBytes:            &sizeBytes,
		BeforeCommitHash:     beforeCommitHash,
		AfterCommitHash:      afterCommitHash,
	}
	if err := conn.Create(&record).Error; err != nil {
		return 0, fmt.Errorf("failed to insert %s report: %w", reportType, err)
	}
	return int64(record.ProjectAnalysisReportsID), nil
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

// GetLatestProjectKBReport는 projectID 기준 가장 최신 PROJECT_KB 리포트를 조회합니다.
// 없으면 nil, nil을 반환합니다.
func GetLatestProjectKBReport(projectID int64) (*ProjectAnalysisReport, error) {
	var report ProjectAnalysisReport
	err := conn.Where("project_id = ? AND report_type = 'PROJECT_KB'", projectID).
		Order("version DESC").
		First(&report).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest PROJECT_KB report: %w", err)
	}
	return &report, nil
}

// ClaimAnalysisJob는 job_status가 ANALYSIS_JOB_QUEUED인 경우에만 ANALYSIS_JOB_RUNNING으로 변경합니다.
// 다른 워커가 이미 선점한 경우 false를 반환합니다.
func ClaimAnalysisJob(jobID int64) (bool, error) {
	result := conn.Model(&AnalysisJob{}).
		Where("analysis_job_id = ? AND job_status = 'ANALYSIS_JOB_QUEUED'", jobID).
		Updates(map[string]any{
			"job_status":   "ANALYSIS_JOB_RUNNING",
			"processed_at": time.Now(),
		})
	if result.Error != nil {
		return false, fmt.Errorf("failed to claim analysis job: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

// UpdateAnalysisJobStatus는 analysis_jobs의 job_status를 업데이트합니다.
func UpdateAnalysisJobStatus(jobID int64, status string) error {
	err := conn.Model(&AnalysisJob{}).
		Where("analysis_job_id = ?", jobID).
		Update("job_status", status).Error
	if err != nil {
		return fmt.Errorf("failed to update analysis job status: %w", err)
	}
	return nil
}
