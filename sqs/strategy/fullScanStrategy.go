package strategy

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"worker_GoVer/apperrors"
	"worker_GoVer/artifact/codeContent"
	"worker_GoVer/artifact/codeGraph"
	codeGraphStrategy "worker_GoVer/artifact/codeGraph/strategy"
	"worker_GoVer/artifact/projectContext"
	"worker_GoVer/artifact/roadmap"
	"worker_GoVer/artifact/userView"
	"worker_GoVer/config"
	"worker_GoVer/db"
	"worker_GoVer/disk"
	"worker_GoVer/git"
	"worker_GoVer/logger"
	"worker_GoVer/quest"
	"worker_GoVer/s3"
)

func (s FullScanStrategy) Handle(ctx context.Context, base SqsBaseMessage) (*StrategyResult, error) {
	startAt := time.Now()

	dataBytes, err := json.Marshal(base.Data)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrUnmarshalMessage, 400, false, err, "failed to marshal FullScan data")
	}
	var msg FullScanQueueMessage
	if err := json.Unmarshal(dataBytes, &msg); err != nil {
		return nil, apperrors.Newf(apperrors.ErrUnmarshalMessage, 400, false, err, "failed to unmarshal FullScan message")
	}

	jobID := base.JobID
	logger.AnalysisStarted(ctx, jobID,
		slog.String("analysisType", "FULL_SCAN_ANALYSIS_REQUEST"),
		slog.String("repo", msg.RepositoryFullName),
		slog.String("branch", msg.BranchName),
	)

	cfg := config.Get()
	jobIDInt, _ := strconv.ParseInt(jobID, 10, 64)
	projectMeta := projectContext.ProjectMetadata{
		Title:       msg.ProjectTitle,
		Description: msg.ProjectDescription,
		Goal:        msg.ProjectGoal,
	}

	var rb rollbackList
	jobClaimed := false

	fail := func(err error) error {
		logger.AnalysisFailed(ctx, jobID, err, time.Since(startAt).Milliseconds(),
			slog.String("analysisType", "FULL_SCAN_ANALYSIS_REQUEST"),
			slog.String("repo", msg.RepositoryFullName),
		)
		rb.Run(ctx)
		if jobClaimed {
			if dbErr := db.UpdateAnalysisJobStatus(jobIDInt, "NOTIFICATION_QUEUED"); dbErr != nil {
				logger.Warn(ctx, "rollback: failed to mark job as NOTIFICATION_QUEUED", slog.String("reason", dbErr.Error()))
			}
		}
		return err
	}

	// 1. job 선점: ANALYSIS_JOB_QUEUED → ANALYSIS_JOB_RUNNING
	claimStep := logger.StepStart(ctx, "job.claim", jobID)
	ok, err := db.ClaimAnalysisJob(jobIDInt)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "failed to claim job jobId=%s", jobID)
		claimStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	if !ok {
		claimStep.Complete(slog.String("status", "skipped"))
		logger.Warn(ctx, "analysis job already claimed, skipping")
		return nil, nil
	}
	claimStep.Complete()
	jobClaimed = true

	// 2. 디스크 정리
	cleanupStep := logger.StepStart(ctx, "workspace.cleanup", jobID)
	if err := disk.IfNeedDoCleanWorkspace(ctx); err != nil {
		cleanupStep.Fail(err)
		logger.Warn(ctx, "workspace cleanup warning", slog.String("reason", err.Error()))
	} else {
		cleanupStep.Complete()
	}

	// 3. 로컬 경로 결정
	localPath := filepath.Join(cfg.WorkspaceBaseDir,
		strconv.FormatInt(msg.InstallationID, 10),
		strconv.FormatInt(msg.RepositoryID, 10),
	)

	// 4. clone or fetch
	repoStep := logger.StepStart(ctx, "git.prepare", jobID, slog.String("localPath", localPath))
	exists, err := disk.IsExistDir(ctx, localPath)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to check dir")
		repoStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	if exists {
		if err := git.Fetch(ctx, localPath, msg.InstallationID); err != nil {
			wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to fetch repo=%s", msg.RepositoryFullName)
			repoStep.Fail(wrapped)
			return nil, fail(wrapped)
		}
	} else {
		if err := git.CloneRepository(ctx, msg.InstallationID, msg.RepositoryFullName, localPath, msg.BranchName); err != nil {
			wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to clone repo=%s branch=%s", msg.RepositoryFullName, msg.BranchName)
			repoStep.Fail(wrapped)
			return nil, fail(wrapped)
		}
	}
	repoStep.Complete(slog.Bool("exists", exists))

	// 5. 브랜치 체크아웃
	checkoutStep := logger.StepStart(ctx, "git.checkout_branch", jobID, slog.String("branch", msg.BranchName))
	if err := git.CheckoutBranch(ctx, localPath, msg.BranchName); err != nil {
		wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to checkout branch=%s", msg.BranchName)
		checkoutStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	checkoutStep.Complete()

	// 6. lock
	lockStep := logger.StepStart(ctx, "workspace.lock", jobID)
	locked, err := disk.IsLocked(ctx, localPath)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrWorkspaceLocked, 500, true, err, "failed to check lock jobId=%s", jobID)
		lockStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	if locked {
		wrapped := apperrors.Newf(apperrors.ErrWorkspaceLocked, 409, true, nil, "repository is locked jobId=%s", jobID)
		lockStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	if _, err := disk.CreateLockFileAtomic(ctx, localPath); err != nil {
		wrapped := apperrors.Newf(apperrors.ErrWorkspaceLocked, 500, true, err, "failed to acquire lock jobId=%s", jobID)
		lockStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	lockStep.Complete()
	defer func() {
		if err := disk.RemoveLockAtomic(ctx, localPath); err != nil {
			logger.Warn(ctx, "failed to remove workspace lock", slog.String("reason", err.Error()))
		}
	}()

	// 7. CodeGraph 생성
	graphStep := logger.StepStart(ctx, "codegraph.generate", jobID)
	graphPath, err := codeGraph.GenerateCodeGraph(ctx, localPath)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to generate code graph repo=%s", msg.RepositoryFullName)
		graphStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	graphStep.Complete()

	// 8. CodeContent 생성
	contentStep := logger.StepStart(ctx, "codecontent.generate", jobID)
	graphData, err := os.ReadFile(graphPath)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to read code graph")
		contentStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	var graph codeGraphStrategy.CodeGraph
	if err := json.Unmarshal(graphData, &graph); err != nil {
		wrapped := apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to parse code graph")
		contentStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	contentPath, err := codeContent.GenerateCodeContent(ctx, localPath, &graph)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to generate code content")
		contentStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	contentStep.Complete()

	// 9. ProjectContext 생성
	ctxVersion, err := db.NextReportVersion(msg.ProjectID, "PROJECT_KB")
	if err != nil {
		logger.Warn(ctx, "failed to get project context version, defaulting to 1", slog.String("reason", err.Error()))
		ctxVersion = 1
	}
	projectContextStep := logger.StepStart(ctx, "project_context.generate", jobID, slog.Int("version", ctxVersion))
	ctxPath, err := projectContext.GenerateProjectContext(ctx, localPath, graphPath, contentPath, ctxVersion, projectMeta)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to generate project context")
		projectContextStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	projectContextStep.Complete()

	// 10. ProjectContext S3 업로드 + DB 저장
	result := &StrategyResult{}
	persistStep := logger.StepStart(ctx, "project_context.persist", jobID, slog.Int("version", ctxVersion))
	kbID, kbURL, err := projectContext.Persist(ctx, ctxPath, nil, msg.InstallationID, msg.RepositoryID, msg.ProjectID, ctxVersion, cfg.AWSS3Bucket, "", "")
	if err != nil {
		persistStep.Fail(err)
		// S3 업로드는 성공했지만 DB 실패인 경우 S3 오브젝트를 즉시 정리
		if kbURL != "" {
			if delErr := s3.DeleteObject(ctx, cfg.AWSS3Bucket, kbURL); delErr != nil {
				logger.Warn(ctx, "rollback: failed to delete orphaned PROJECT_KB from S3", slog.String("reason", delErr.Error()))
			}
		}
		return nil, fail(apperrors.Newf(apperrors.ErrS3Operation, 500, true, err, "failed to persist project context"))
	}
	result.NewProjectKBID = &kbID
	persistStep.Complete(slog.Int64("projectKbId", kbID))
	rb.Add(func() {
		if delErr := s3.DeleteObject(ctx, cfg.AWSS3Bucket, kbURL); delErr != nil {
			logger.Warn(ctx, "rollback: failed to delete PROJECT_KB from S3", slog.String("reason", delErr.Error()))
		}
		if delErr := db.DeleteAnalysisReport(kbID); delErr != nil {
			logger.Warn(ctx, "rollback: failed to delete PROJECT_KB report from DB", slog.String("reason", delErr.Error()))
		}
	})

	// 11. RoadMap AI 생성 (DB replace는 user view 저장 이후 수행)
	roadMapStep := logger.StepStart(ctx, "roadmap.generate", jobID)
	roadMapPlan, err := roadmap.Generate(ctx, roadmap.GenerateInput{
		ProjectID:          msg.ProjectID,
		UserID:             base.UserID,
		ProjectTitle:       msg.ProjectTitle,
		ProjectDescription: msg.ProjectDescription,
		ProjectGoal:        msg.ProjectGoal,
		RepositoryFullName: msg.RepositoryFullName,
		BranchName:         msg.BranchName,
	}, ctxPath)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to generate roadmap")
		roadMapStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	roadMapStep.Complete(
		slog.Int("phaseCount", len(roadMapPlan.Phases)),
		slog.Int("milestoneCount", len(roadMapPlan.Milestones)),
	)

	// 12. Quest 평가 및 생성
	questStep := logger.StepStart(ctx, "quest.generate", jobID)
	questReq, err := quest.BuildQuestRequest(ctx, jobIDInt, msg.ProjectID, base.UserID, msg.ProjectTitle, msg.ProjectDescription, msg.ProjectGoal, msg.RepositoryFullName, msg.BranchName)
	if err != nil {
		questStep.Fail(err)
		return nil, fail(apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "failed to build quest request"))
	}
	questReq.RoadMapMilestones = roadMapPlan.ToQuestMilestones()
	questResp, err := quest.GenerateAndEvaluateQuests(ctx, ctxPath, questReq)
	if err != nil {
		questStep.Fail(err)
		return nil, fail(apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to generate quests"))
	}
	var questMilestoneLinks []db.QuestMilestoneLinkInput
	result.CompleteQuestIDs, result.NewQuestIDs, questMilestoneLinks = quest.SaveResults(ctx, jobIDInt, msg.ProjectID, base.UserID, questReq, questResp)
	rb.Add(func() {
		if delErr := db.DeleteQuestEvaluationsByJobID(jobIDInt); delErr != nil {
			logger.Warn(ctx, "rollback: failed to delete quest evaluations", slog.String("reason", delErr.Error()))
		}
		if delErr := db.DeleteQuestsByIDs(result.NewQuestIDs); delErr != nil {
			logger.Warn(ctx, "rollback: failed to delete new quests", slog.String("reason", delErr.Error()))
		}
		if delErr := db.RevertQuestCompletion(result.CompleteQuestIDs); delErr != nil {
			logger.Warn(ctx, "rollback: failed to revert quest completion", slog.String("reason", delErr.Error()))
		}
	})
	questStep.Complete(
		slog.Int("completedQuestCount", len(result.CompleteQuestIDs)),
		slog.Int("newQuestCount", len(result.NewQuestIDs)),
	)

	// 13. UserView 생성
	uvVersion, err := db.NextReportVersion(msg.ProjectID, "USER_VIEW")
	if err != nil {
		logger.Warn(ctx, "failed to get user view version, defaulting to 1", slog.String("reason", err.Error()))
		uvVersion = 1
	}
	uvInput := userView.GenerateInput{
		ProjectID:          msg.ProjectID,
		UserID:             base.UserID,
		ProjectTitle:       msg.ProjectTitle,
		ProjectDescription: msg.ProjectDescription,
		ProjectGoal:        msg.ProjectGoal,
		RepositoryFullName: msg.RepositoryFullName,
		BranchName:         msg.BranchName,
		Version:            uvVersion,
	}
	userViewStep := logger.StepStart(ctx, "user_view.generate", jobID, slog.Int("version", uvVersion))
	uvPath, err := userView.Generate(ctx, uvInput, ctxPath, localPath)
	if err != nil {
		userViewStep.Fail(err)
		return nil, fail(apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to generate user view"))
	}
	uvID, uvURL, err := userView.Persist(ctx, uvPath, result.NewProjectKBID, msg.InstallationID, msg.RepositoryID, msg.ProjectID, uvVersion, cfg.AWSS3Bucket, "", "")
	if err != nil {
		userViewStep.Fail(err)
		if uvURL != "" {
			if delErr := s3.DeleteObject(ctx, cfg.AWSS3Bucket, uvURL); delErr != nil {
				logger.Warn(ctx, "rollback: failed to delete orphaned USER_VIEW from S3", slog.String("reason", delErr.Error()))
			}
		}
		return nil, fail(apperrors.Newf(apperrors.ErrS3Operation, 500, true, err, "failed to persist user view"))
	}
	result.UserViewReportID = &uvID
	userViewStep.Complete(slog.Int64("userViewReportId", uvID))
	rb.Add(func() {
		if delErr := s3.DeleteObject(ctx, cfg.AWSS3Bucket, uvURL); delErr != nil {
			logger.Warn(ctx, "rollback: failed to delete USER_VIEW from S3", slog.String("reason", delErr.Error()))
		}
		if delErr := db.DeleteAnalysisReport(uvID); delErr != nil {
			logger.Warn(ctx, "rollback: failed to delete USER_VIEW report from DB", slog.String("reason", delErr.Error()))
		}
	})

	// 14. RoadMap DB replace + 신규 퀘스트 milestone 연결
	roadMapPersistStep := logger.StepStart(ctx, "roadmap.persist", jobID)
	roadMapSaveResult, err := roadmap.Persist(ctx, msg.ProjectID, roadMapPlan, questMilestoneLinks)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "failed to persist roadmap")
		roadMapPersistStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	roadMapPersistStep.Complete(
		slog.Int("phaseCount", len(roadMapSaveResult.PhaseIDs)),
		slog.Int("milestoneCount", len(roadMapSaveResult.MilestoneIDs)),
		slog.Int("linkedQuestCount", len(roadMapSaveResult.LinkedQuestIDs)),
		slog.Int("skippedQuestLinkCount", len(roadMapSaveResult.SkippedQuestLinks)),
	)

	// 15. touch 파일 업데이트
	touchStep := logger.StepStart(ctx, "workspace.touch", jobID)
	if _, err := disk.CreateTouchFileAtomic(ctx, localPath); err != nil {
		touchStep.Fail(err)
		logger.Warn(ctx, "failed to update touch file", slog.String("reason", err.Error()))
	} else {
		touchStep.Complete()
	}

	// 16. 작업 완료
	statusStep := logger.StepStart(ctx, "job.complete", jobID)
	if err := db.UpdateAnalysisJobStatus(jobIDInt, "ANALYSIS_JOB_COMPLETED"); err != nil {
		statusStep.Fail(err)
		logger.Warn(ctx, "failed to update analysis job status to completed", slog.String("reason", err.Error()))
	} else {
		statusStep.Complete()
	}

	logger.AnalysisCompleted(ctx, jobID, time.Since(startAt).Milliseconds(),
		slog.String("analysisType", "FULL_SCAN_ANALYSIS_REQUEST"),
		slog.String("repo", msg.RepositoryFullName),
	)
	return result, nil
}
