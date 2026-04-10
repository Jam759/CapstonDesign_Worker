package strategy

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strconv"
	"time"
	"worker_GoVer/apperrors"
	"worker_GoVer/artifact/codeGraph"
	"worker_GoVer/artifact/projectContext"
	"worker_GoVer/artifact/userView"
	"worker_GoVer/config"
	"worker_GoVer/db"
	"worker_GoVer/disk"
	"worker_GoVer/git"
	"worker_GoVer/logger"
	"worker_GoVer/quest"
	"worker_GoVer/s3"
)

func (s NormalAnalysisStrategy) Handle(ctx context.Context, jobID string, data json.RawMessage) (*StrategyResult, error) {
	startAt := time.Now()

	var msg NormalAnalysisQueueMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, apperrors.Newf(apperrors.ErrUnmarshalMessage, 400, false, err, "failed to unmarshal NormalAnalysis message")
	}

	logger.AnalysisStarted(ctx, jobID,
		slog.String("analysisType", "NORMAL_ANALYSIS_REQUEST"),
		slog.String("repo", msg.RepositoryFullName),
		slog.String("branch", msg.BranchName),
		slog.Bool("isMerge", msg.IsMerge),
	)

	cfg := config.Get()
	jobIDInt := msg.JobID
	if jobIDInt == 0 {
		jobIDInt, _ = strconv.ParseInt(jobID, 10, 64)
	}

	var rb rollbackList
	jobClaimed := false

	fail := func(err error) error {
		logger.AnalysisFailed(ctx, jobID, err, time.Since(startAt).Milliseconds(),
			slog.String("analysisType", "NORMAL_ANALYSIS_REQUEST"),
			slog.String("repo", msg.RepositoryFullName),
		)
		rb.Run(ctx)
		if jobClaimed {
			if dbErr := db.UpdateAnalysisJobStatus(jobIDInt, "ANALYSIS_JOB_FAILED"); dbErr != nil {
				logger.Warn(ctx, "rollback: failed to mark job as FAILED", slog.String("reason", dbErr.Error()))
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
		strconv.FormatInt(msg.PushUserInstallationID, 10),
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
		if err := git.Fetch(ctx, localPath, msg.PushUserInstallationID); err != nil {
			wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to fetch repo=%s", msg.RepositoryFullName)
			repoStep.Fail(wrapped)
			return nil, fail(wrapped)
		}
	} else {
		if err := git.CloneRepository(ctx, msg.PushUserInstallationID, msg.RepositoryFullName, localPath, msg.BranchName); err != nil {
			wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to clone repo=%s branch=%s", msg.RepositoryFullName, msg.BranchName)
			repoStep.Fail(wrapped)
			return nil, fail(wrapped)
		}
	}
	repoStep.Complete(slog.Bool("exists", exists))

	// 5. 브랜치 체크아웃
	checkoutBranchStep := logger.StepStart(ctx, "git.checkout_branch", jobID, slog.String("branch", msg.BranchName))
	if err := git.CheckoutBranch(ctx, localPath, msg.BranchName); err != nil {
		wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to checkout branch=%s", msg.BranchName)
		checkoutBranchStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	checkoutBranchStep.Complete()

	// 6. afterCommit checkout
	checkoutCommitStep := logger.StepStart(ctx, "git.checkout_commit", jobID, slog.String("afterCommit", msg.AfterCommit))
	if err := git.Checkout(ctx, localPath, msg.AfterCommit); err != nil {
		wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to checkout afterCommit=%s", msg.AfterCommit)
		checkoutCommitStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	checkoutCommitStep.Complete()

	// 7. lock
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

	// 8. 최신 PROJECT_KB 조회
	latestKB, err := db.GetLatestProjectKBReport(msg.ProjectID)
	if err != nil {
		return nil, fail(apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "failed to get latest PROJECT_KB projectId=%d", msg.ProjectID))
	}
	if latestKB == nil {
		return nil, fail(apperrors.Newf(apperrors.ErrNoProjectKB, 422, false, nil, "no PROJECT_KB found for projectId=%d", msg.ProjectID))
	}

	baseCommit := msg.BeforeCommit
	if latestKB.AfterCommitHash != "" && latestKB.AfterCommitHash != msg.BeforeCommit {
		baseCommit = latestKB.AfterCommitHash
	}

	// 9. baseline PROJECT_KB S3 다운로드
	artifactDir := filepath.Join(localPath, "artifact")
	downloadStep := logger.StepStart(ctx, "project_context.download", jobID)
	baselinePath, err := s3.DownloadProjectKB(ctx, latestKB.S3Bucket, latestKB.StoredURL, artifactDir)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrS3Operation, 500, true, err, "failed to download baseline KB projectId=%d", msg.ProjectID)
		downloadStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	downloadStep.Complete()

	// 10. git diff 생성
	diffStep := logger.StepStart(ctx, "git.diff", jobID)
	diffPath, err := git.Diff(ctx, localPath, baseCommit, msg.AfterCommit, msg.IsMerge)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to generate diff before=%s after=%s", baseCommit, msg.AfterCommit)
		diffStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	diffStep.Complete()

	// 11. 변경 파일 목록
	diffFileStep := logger.StepStart(ctx, "git.diff_files", jobID)
	diffFiles, err := git.DiffFileList(ctx, localPath, baseCommit, msg.AfterCommit, msg.IsMerge)
	if err != nil {
		diffFileStep.Fail(err)
		logger.Warn(ctx, "failed to get diff file list", slog.String("reason", err.Error()))
	} else {
		diffFileStep.Complete(slog.Int("fileCount", len(diffFiles)))
	}
	changedPaths := make([]string, 0, len(diffFiles))
	for _, f := range diffFiles {
		changedPaths = append(changedPaths, f.Path)
		if f.PreviousPath != "" {
			changedPaths = append(changedPaths, f.PreviousPath)
		}
	}

	// 12. CodeGraph 생성
	graphStep := logger.StepStart(ctx, "codegraph.generate", jobID)
	graphPath, err := codeGraph.GenerateCodeGraph(ctx, localPath)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to generate code graph repo=%s", msg.RepositoryFullName)
		graphStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	graphStep.Complete()

	// 13. incremental ProjectContext 업데이트
	kbVersion, err := db.NextReportVersion(msg.ProjectID, "PROJECT_KB")
	if err != nil {
		logger.Warn(ctx, "failed to get project KB version, defaulting to 1", slog.String("reason", err.Error()))
		kbVersion = 1
	}
	updateContextStep := logger.StepStart(ctx, "project_context.update", jobID, slog.Int("version", kbVersion))
	ctxPath, err := projectContext.UpdateProjectContext(
		ctx,
		localPath,
		baselinePath,
		diffPath,
		graphPath,
		changedPaths,
		baseCommit,
		msg.AfterCommit,
		kbVersion,
	)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to update project context")
		updateContextStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	updateContextStep.Complete()

	// 14. ProjectKB S3 업로드 + DB 저장
	result := &StrategyResult{}
	previousKBID := int64(latestKB.ProjectAnalysisReportsID)
	persistStep := logger.StepStart(ctx, "project_context.persist", jobID, slog.Int("version", kbVersion))
	newKBID, newKBURL, err := projectContext.Persist(ctx, ctxPath, &previousKBID, msg.PushUserInstallationID, msg.RepositoryID, msg.ProjectID, kbVersion, cfg.AWSS3Bucket, baseCommit, msg.AfterCommit)
	if err != nil {
		persistStep.Fail(err)
		if newKBURL != "" {
			if delErr := s3.DeleteObject(ctx, cfg.AWSS3Bucket, newKBURL); delErr != nil {
				logger.Warn(ctx, "rollback: failed to delete orphaned PROJECT_KB from S3", slog.String("reason", delErr.Error()))
			}
		}
		return nil, fail(apperrors.Newf(apperrors.ErrS3Operation, 500, true, err, "failed to persist project context"))
	}
	result.NewProjectKBID = &newKBID
	persistStep.Complete(slog.Int64("projectKbId", newKBID))
	rb.Add(func() {
		if delErr := s3.DeleteObject(ctx, cfg.AWSS3Bucket, newKBURL); delErr != nil {
			logger.Warn(ctx, "rollback: failed to delete PROJECT_KB from S3", slog.String("reason", delErr.Error()))
		}
		if delErr := db.DeleteAnalysisReport(newKBID); delErr != nil {
			logger.Warn(ctx, "rollback: failed to delete PROJECT_KB report from DB", slog.String("reason", delErr.Error()))
		}
	})

	// 15. Quest 평가 및 생성
	questStep := logger.StepStart(ctx, "quest.generate", jobID)
	questReq, err := quest.BuildQuestRequest(ctx, jobIDInt, msg.ProjectID, msg.PushUserID, msg.RepositoryFullName, msg.BranchName)
	if err != nil {
		questStep.Fail(err)
		return nil, fail(apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "failed to build quest request"))
	}
	questResp, err := quest.GenerateAndEvaluateQuests(ctx, ctxPath, questReq)
	if err != nil {
		questStep.Fail(err)
		return nil, fail(apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to generate quests"))
	}
	result.CompleteQuestIDs, result.NewQuestIDs = quest.SaveResults(ctx, jobIDInt, msg.ProjectID, msg.PushUserID, questReq, questResp)
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

	// 16. UserView 생성
	uvVersion, err := db.NextReportVersion(msg.ProjectID, "USER_VIEW")
	if err != nil {
		logger.Warn(ctx, "failed to get user view version, defaulting to 1", slog.String("reason", err.Error()))
		uvVersion = 1
	}
	uvInput := userView.GenerateInput{
		ProjectID:          msg.ProjectID,
		UserID:             msg.PushUserID,
		RepositoryFullName: msg.RepositoryFullName,
		BranchName:         msg.BranchName,
		BeforeCommitHash:   baseCommit,
		AfterCommitHash:    msg.AfterCommit,
		Version:            uvVersion,
		CompletedQuestIDs:  result.CompleteQuestIDs,
		NewQuestIDs:        result.NewQuestIDs,
	}
	userViewStep := logger.StepStart(ctx, "user_view.generate", jobID, slog.Int("version", uvVersion))
	uvPath, err := userView.Generate(ctx, uvInput, ctxPath, localPath)
	if err != nil {
		userViewStep.Fail(err)
		return nil, fail(apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to generate user view"))
	}
	uvID, uvURL, err := userView.Persist(ctx, uvPath, result.NewProjectKBID, msg.PushUserInstallationID, msg.RepositoryID, msg.ProjectID, uvVersion, cfg.AWSS3Bucket, baseCommit, msg.AfterCommit)
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

	// 17. touch 파일 업데이트
	touchStep := logger.StepStart(ctx, "workspace.touch", jobID)
	if _, err := disk.CreateTouchFileAtomic(ctx, localPath); err != nil {
		touchStep.Fail(err)
		logger.Warn(ctx, "failed to update touch file", slog.String("reason", err.Error()))
	} else {
		touchStep.Complete()
	}

	// 18. 작업 완료
	statusStep := logger.StepStart(ctx, "job.complete", jobID)
	if err := db.UpdateAnalysisJobStatus(jobIDInt, "ANALYSIS_JOB_COMPLETED"); err != nil {
		statusStep.Fail(err)
		logger.Warn(ctx, "failed to update analysis job status to completed", slog.String("reason", err.Error()))
	} else {
		statusStep.Complete()
	}

	logger.AnalysisCompleted(ctx, jobID, time.Since(startAt).Milliseconds(),
		slog.String("analysisType", "NORMAL_ANALYSIS_REQUEST"),
		slog.String("repo", msg.RepositoryFullName),
	)
	return result, nil
}

