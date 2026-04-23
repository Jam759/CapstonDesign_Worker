package db

import (
	"errors"
	"fmt"
	"strings"
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
	category string,
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
		Category:           category,
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

func ReplaceProjectRoadMap(projectID int64, phases []RoadMapPhaseInput, questLinks []QuestMilestoneLinkInput) (*RoadMapSaveResult, error) {
	if len(phases) == 0 {
		return nil, fmt.Errorf("roadmap must contain at least one phase")
	}

	result := &RoadMapSaveResult{
		PhaseIDsByKey:     make(map[string]int64, len(phases)),
		MilestoneIDsByKey: make(map[string]int64),
		PhaseIDs:          make([]int64, 0, len(phases)),
		MilestoneIDs:      make([]int64, 0),
		LinkedQuestIDs:    make([]int64, 0, len(questLinks)),
		SkippedQuestLinks: make([]QuestMilestoneLinkInput, 0),
	}

	err := conn.Transaction(func(tx *gorm.DB) error {
		if err := deleteProjectRoadMapTx(tx, projectID); err != nil {
			return err
		}

		for _, phase := range phases {
			phaseKey := strings.TrimSpace(phase.Key)
			if phaseKey == "" {
				return fmt.Errorf("roadmap phase key is required")
			}

			phaseRecord := ProjectPhase{
				ProjectID:      projectID,
				PhaseOrder:     phase.PhaseOrder,
				PhaseName:      phase.PhaseName,
				PhaseObjective: phase.PhaseObjective,
				PhaseOutcome:   phase.PhaseOutcome,
				ExitCriteria:   phase.ExitCriteria,
				Status:         phase.Status,
			}
			if err := tx.Create(&phaseRecord).Error; err != nil {
				return fmt.Errorf("failed to insert roadmap phase key=%s: %w", phaseKey, err)
			}
			result.PhaseIDsByKey[phaseKey] = phaseRecord.ProjectPhaseID
			result.PhaseIDs = append(result.PhaseIDs, phaseRecord.ProjectPhaseID)

			for idx, scope := range phase.PhaseScopes {
				scope = strings.TrimSpace(scope)
				if scope == "" {
					continue
				}
				scopeRecord := ProjectPhaseScope{
					ProjectPhaseID: phaseRecord.ProjectPhaseID,
					ScopeOrder:     idx,
					Scope:          scope,
				}
				if err := tx.Create(&scopeRecord).Error; err != nil {
					return fmt.Errorf("failed to insert roadmap phase scope phaseKey=%s: %w", phaseKey, err)
				}
			}

			for _, milestone := range phase.Milestones {
				milestoneKey := strings.TrimSpace(milestone.Key)
				if milestoneKey == "" {
					return fmt.Errorf("roadmap milestone key is required phaseKey=%s", phaseKey)
				}

				milestoneRecord := ProjectMilestone{
					ProjectID:        projectID,
					PhaseID:          phaseRecord.ProjectPhaseID,
					MilestoneName:    milestone.MilestoneName,
					MilestoneIntent:  milestone.MilestoneIntent,
					TriggerCondition: milestone.TriggerCondition,
					ExpectedState:    milestone.ExpectedState,
					CompletionRule:   milestone.CompletionRule,
					Status:           milestone.Status,
				}
				if err := tx.Create(&milestoneRecord).Error; err != nil {
					return fmt.Errorf("failed to insert roadmap milestone key=%s: %w", milestoneKey, err)
				}
				result.MilestoneIDsByKey[milestoneKey] = milestoneRecord.ProjectMilestoneID
				result.MilestoneIDs = append(result.MilestoneIDs, milestoneRecord.ProjectMilestoneID)

				for idx, evidence := range milestone.ObservableEvidence {
					evidence = strings.TrimSpace(evidence)
					if evidence == "" {
						continue
					}
					evidenceRecord := ProjectMilestoneObservableEvidence{
						ProjectMilestoneID: milestoneRecord.ProjectMilestoneID,
						EvidenceOrder:      idx,
						Evidence:           evidence,
					}
					if err := tx.Create(&evidenceRecord).Error; err != nil {
						return fmt.Errorf("failed to insert roadmap milestone evidence milestoneKey=%s: %w", milestoneKey, err)
					}
				}
			}
		}

		for _, link := range questLinks {
			milestoneKey := strings.TrimSpace(link.MilestoneKey)
			milestoneID, ok := result.MilestoneIDsByKey[milestoneKey]
			if link.UserAiQuestID <= 0 || milestoneKey == "" || !ok {
				result.SkippedQuestLinks = append(result.SkippedQuestLinks, link)
				continue
			}

			update := tx.Model(&UserAiQuest{}).
				Where("user_ai_quest_id = ? AND project_id = ?", link.UserAiQuestID, projectID).
				Update("related_milestone_id", milestoneID)
			if update.Error != nil {
				return fmt.Errorf("failed to link quest=%d to milestone=%d: %w", link.UserAiQuestID, milestoneID, update.Error)
			}
			if update.RowsAffected == 0 {
				result.SkippedQuestLinks = append(result.SkippedQuestLinks, link)
				continue
			}
			result.LinkedQuestIDs = append(result.LinkedQuestIDs, link.UserAiQuestID)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to replace roadmap: %w", err)
	}

	return result, nil
}

func deleteProjectRoadMapTx(tx *gorm.DB, projectID int64) error {
	var milestoneIDs []int64
	if err := tx.Model(&ProjectMilestone{}).
		Where("project_id = ?", projectID).
		Pluck("project_milestone_id", &milestoneIDs).Error; err != nil {
		return fmt.Errorf("failed to query roadmap milestones: %w", err)
	}

	if len(milestoneIDs) > 0 {
		if err := tx.Model(&UserAiQuest{}).
			Where("project_id = ? AND related_milestone_id IN ?", projectID, milestoneIDs).
			Update("related_milestone_id", gorm.Expr("NULL")).Error; err != nil {
			return fmt.Errorf("failed to clear quest roadmap links: %w", err)
		}
		if err := tx.Delete(&ProjectMilestoneObservableEvidence{}, "project_milestone_id IN ?", milestoneIDs).Error; err != nil {
			return fmt.Errorf("failed to delete milestone evidence: %w", err)
		}
		if err := tx.Delete(&ProjectMilestone{}, "project_milestone_id IN ?", milestoneIDs).Error; err != nil {
			return fmt.Errorf("failed to delete milestones: %w", err)
		}
	}

	var phaseIDs []int64
	if err := tx.Model(&ProjectPhase{}).
		Where("project_id = ?", projectID).
		Pluck("project_phase_id", &phaseIDs).Error; err != nil {
		return fmt.Errorf("failed to query roadmap phases: %w", err)
	}

	if len(phaseIDs) > 0 {
		if err := tx.Delete(&ProjectPhaseScope{}, "project_phase_id IN ?", phaseIDs).Error; err != nil {
			return fmt.Errorf("failed to delete phase scopes: %w", err)
		}
		if err := tx.Delete(&ProjectPhase{}, "project_phase_id IN ?", phaseIDs).Error; err != nil {
			return fmt.Errorf("failed to delete phases: %w", err)
		}
	}

	return nil
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
func GetAnalysisJobDispatchInput(jobID int64) (*AnalysisJobDispatchInput, error) {
	var input AnalysisJobDispatchInput
	err := conn.Table("analysis_jobs AS aj").
		Select(`
			aj.analysis_job_id,
			aj.project_id,
			aj.user_id,
			aj.github_app_installation_id,
			aj.installation_repository_id,
			aj.before_commit_hash,
			aj.after_commit_hash,
			aj.branch,
			aj.analysis_event_type,
			aj.is_private_repo,
			aj.is_merge,
			COALESCE(p.title, '') AS project_title,
			COALESCE(p.description, '') AS project_description,
			COALESCE(p.goal, '') AS project_goal,
			ir.full_name AS repository_full_name
		`).
		Joins("LEFT JOIN projects AS p ON p.project_id = aj.project_id").
		Joins("LEFT JOIN installation_repository AS ir ON ir.installation_repository_id = aj.installation_repository_id").
		Where("aj.analysis_job_id = ?", jobID).
		Take(&input).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get analysis job dispatch input: %w", err)
	}
	return &input, nil
}

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

// DeleteAnalysisReport는 project_analysis_reports 레코드를 삭제합니다. (롤백용)
func DeleteAnalysisReport(id int64) error {
	err := conn.Delete(&ProjectAnalysisReport{}, "project_analysis_reports_id = ?", id).Error
	if err != nil {
		return fmt.Errorf("failed to delete analysis report id=%d: %w", id, err)
	}
	return nil
}

// DeleteQuestEvaluationsByJobID는 특정 jobID에 해당하는 평가 레코드를 전부 삭제합니다. (롤백용)
func DeleteQuestEvaluationsByJobID(jobID int64) error {
	err := conn.Delete(&UserAiQuestEvaluation{}, "analysis_job_id = ?", jobID).Error
	if err != nil {
		return fmt.Errorf("failed to delete quest evaluations jobId=%d: %w", jobID, err)
	}
	return nil
}

// DeleteQuestsByIDs는 신규 생성된 퀘스트 레코드를 삭제합니다. (롤백용)
func DeleteQuestsByIDs(questIDs []int64) error {
	if len(questIDs) == 0 {
		return nil
	}
	err := conn.Delete(&UserAiQuest{}, "user_ai_quest_id IN ?", questIDs).Error
	if err != nil {
		return fmt.Errorf("failed to delete quests ids=%v: %w", questIDs, err)
	}
	return nil
}

// RevertQuestCompletion은 COMPLETED 처리된 퀘스트를 ACTIVE 상태로 되돌립니다. (롤백용)
func RevertQuestCompletion(questIDs []int64) error {
	if len(questIDs) == 0 {
		return nil
	}
	err := conn.Model(&UserAiQuest{}).
		Where("user_ai_quest_id IN ?", questIDs).
		Updates(map[string]any{
			"progress_status": "ACTIVE",
			"approval_status": "REQUEST_ACCEPT",
			"completed_at":    nil,
		}).Error
	if err != nil {
		return fmt.Errorf("failed to revert quest completion ids=%v: %w", questIDs, err)
	}
	return nil
}

// FetchActiveMilestones는 projectID 기준 PENDING/IN_PROGRESS 마일스톤을 phase 정보와 함께 조회합니다.
func FetchActiveMilestones(projectID int64) ([]ActiveMilestone, error) {
	var milestones []ActiveMilestone
	err := conn.Table("project_milestone AS pm").
		Select(`pm.project_milestone_id, pp.phase_name, pm.milestone_name,
			pm.milestone_intent, pm.trigger_condition, pm.expected_state,
			pm.completion_rule, pm.status`).
		Joins("JOIN project_phase AS pp ON pp.project_phase_id = pm.phase_id").
		Where("pm.project_id = ? AND pm.status IN ('PENDING', 'IN_PROGRESS')", projectID).
		Scan(&milestones).Error
	if err != nil {
		return nil, fmt.Errorf("failed to fetch active milestones: %w", err)
	}
	return milestones, nil
}

// InsertMilestoneEvaluation는 마일스톤 평가 결과를 저장합니다.
func InsertMilestoneEvaluation(milestoneID int64, jobID int64, evaluationResult string, confidenceScore float64, reason string, progressNote string) error {
	now := time.Now()
	record := ProjectMilestoneEvaluation{
		ProjectMilestoneID: milestoneID,
		AnalysisJobID:      &jobID,
		EvaluationResult:   evaluationResult,
		ConfidenceScore:    confidenceScore,
		Reason:             reason,
		ProgressNote:       progressNote,
		EvaluatedAt:        now,
	}
	if err := conn.Create(&record).Error; err != nil {
		return fmt.Errorf("failed to insert milestone evaluation: %w", err)
	}
	return nil
}

// UpdateMilestoneStatus는 마일스톤의 status를 업데이트합니다.
func UpdateMilestoneStatus(milestoneID int64, status string) error {
	err := conn.Model(&ProjectMilestone{}).
		Where("project_milestone_id = ?", milestoneID).
		Update("status", status).Error
	if err != nil {
		return fmt.Errorf("failed to update milestone status id=%d: %w", milestoneID, err)
	}
	return nil
}

// DeleteMilestoneEvaluationsByJobID는 jobID에 해당하는 마일스톤 평가 기록을 삭제합니다. (롤백용)
func DeleteMilestoneEvaluationsByJobID(jobID int64) error {
	err := conn.Delete(&ProjectMilestoneEvaluation{}, "analysis_job_id = ?", jobID).Error
	if err != nil {
		return fmt.Errorf("failed to delete milestone evaluations jobId=%d: %w", jobID, err)
	}
	return nil
}

// RevertMilestoneStatuses는 변경된 마일스톤 status를 이전 값으로 되돌립니다. (롤백용)
func RevertMilestoneStatuses(prevStatuses map[int64]string) error {
	for milestoneID, status := range prevStatuses {
		err := conn.Model(&ProjectMilestone{}).
			Where("project_milestone_id = ?", milestoneID).
			Update("status", status).Error
		if err != nil {
			return fmt.Errorf("failed to revert milestone status id=%d: %w", milestoneID, err)
		}
	}
	return nil
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
