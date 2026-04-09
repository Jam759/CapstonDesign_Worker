package strategy

import (
	"context"
	"encoding/json"
	"fmt"
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
	"worker_GoVer/artifact/userView"
	"worker_GoVer/config"
	"worker_GoVer/db"
	"worker_GoVer/disk"
	"worker_GoVer/git"
	"worker_GoVer/logger"
	"worker_GoVer/quest"
)

func (s FullScanStrategy) Handle(ctx context.Context, jobID string, data json.RawMessage) (*StrategyResult, error) {
	startAt := time.Now()

	var msg FullScanQueueMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, apperrors.Newf(apperrors.ErrUnmarshalMessage, 400, false, err, "failed to unmarshal FullScan message")
	}

	logger.AnalysisStarted(ctx, jobID,
		slog.String("analysisType", "FULL_SCAN"),
		slog.String("repo", msg.RepositoryFullName),
		slog.String("branch", msg.BranchName),
	)
	fail := func(err error) error {
		logger.AnalysisFailed(ctx, jobID, err, time.Since(startAt).Milliseconds(),
			slog.String("analysisType", "FULL_SCAN"),
			slog.String("repo", msg.RepositoryFullName),
		)
		return err
	}

	cfg := config.Get()
	jobIDInt := msg.JobID
	if jobIDInt == 0 {
		jobIDInt, _ = strconv.ParseInt(jobID, 10, 64)
	}

	// 1. job 선점: ANALYSIS_JOB_QUEUED → ANALYSIS_JOB_RUNNING
	claimStep := logger.StepStart(ctx, "job.claim", jobID)
	claimed, err := db.ClaimAnalysisJob(jobIDInt)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "failed to claim job jobId=%s", jobID)
		claimStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	if !claimed {
		claimStep.Complete(slog.String("status", "skipped"))
		logger.Warn(ctx, "analysis job already claimed, skipping")
		return nil, nil
	}
	claimStep.Complete()

	// 2. 디스크 정리
	cleanupStep := logger.StepStart(ctx, "workspace.cleanup", jobID)
	if err := disk.IfNeedDoCleanWorkspace(ctx); err != nil {
		cleanupStep.Fail(err)
		logger.Warn(ctx, "workspace cleanup warning", slog.String("reason", err.Error()))
	} else {
		cleanupStep.Complete()
	}

	// 3. 로컬 경로 결정: WorkspaceBaseDir/{installationId}/{repoId}
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

	// 6. CodeGraph 생성
	graphStep := logger.StepStart(ctx, "codegraph.generate", jobID)
	graphPath, err := codeGraph.GenerateCodeGraph(ctx, localPath)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to generate code graph repo=%s", msg.RepositoryFullName)
		graphStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	graphStep.Complete()

	// 7. CodeContent 생성
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

	// 8. ProjectContext 생성 (AI 분석, 파일로 전달)
	ctxVersion, err := db.NextReportVersion(msg.ProjectID, "PROJECT_KB")
	if err != nil {
		logger.Warn(ctx, "failed to get project context version, defaulting to 1", slog.String("reason", err.Error()))
		ctxVersion = 1
	}
	projectContextStep := logger.StepStart(ctx, "project_context.generate", jobID, slog.Int("version", ctxVersion))
	ctxPath, err := projectContext.GenerateProjectContext(ctx, localPath, graphPath, contentPath, ctxVersion)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to generate project context")
		projectContextStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	projectContextStep.Complete()
	/*

		// 8-1. ProjectContext S3 업로드 + DB 저장
		newKBID := projectContext.Persist(ctx, ctxPath, nil, msg.InstallationID, msg.RepositoryID, msg.ProjectID, ctxVersion, cfg.AWSS3Bucket, "", "")

	*/
	persistStep := logger.StepStart(ctx, "project_context.persist", jobID, slog.Int("version", ctxVersion))
	projectKBID := projectContext.Persist(ctx, ctxPath, nil, msg.InstallationID, msg.RepositoryID, msg.ProjectID, ctxVersion, cfg.AWSS3Bucket, "", "")
	result := &StrategyResult{}
	if projectKBID != 0 {
		result.NewProjectKBID = &projectKBID
		persistStep.Complete(slog.Int64("projectKbId", projectKBID))
	} else {
		persistStep.Fail(fmt.Errorf("project context persist returned 0"))
	}

	// 9. Quest 평가 및 생성
	questStep := logger.StepStart(ctx, "quest.generate", jobID)
	questReq, err := quest.BuildQuestRequest(ctx, jobIDInt, msg.ProjectID, msg.UserID, msg.RepositoryFullName, msg.BranchName)
	if err != nil {
		questStep.Fail(err)
		logger.Warn(ctx, "failed to build quest request", slog.String("reason", err.Error()))
	} else {
		questResp, err := quest.GenerateAndEvaluateQuests(ctx, ctxPath, questReq)
		if err != nil {
			questStep.Fail(err)
			logger.Warn(ctx, "failed to generate quests", slog.String("reason", err.Error()))
		} else {
			result.CompleteQuestIDs, result.NewQuestIDs = quest.SaveResults(ctx, jobIDInt, msg.ProjectID, msg.UserID, questReq, questResp)
			questStep.Complete(
				slog.Int("completedQuestCount", len(result.CompleteQuestIDs)),
				slog.Int("newQuestCount", len(result.NewQuestIDs)),
			)
		}
	}

	// 10. UserView 생성
	version, err := db.NextReportVersion(msg.ProjectID, "USER_VIEW")
	if err != nil {
		logger.Warn(ctx, "failed to get user view version, defaulting to 1", slog.String("reason", err.Error()))
		version = 1
	}
	uvInput := userView.GenerateInput{
		ProjectID:          msg.ProjectID,
		UserID:             msg.UserID,
		RepositoryFullName: msg.RepositoryFullName,
		BranchName:         msg.BranchName,
		Version:            version,
	}
	userViewStep := logger.StepStart(ctx, "user_view.generate", jobID, slog.Int("version", version))
	if uvPath, err := userView.Generate(ctx, uvInput, ctxPath, localPath); err != nil {
		userViewStep.Fail(err)
		logger.Warn(ctx, "failed to generate user view", slog.String("reason", err.Error()))
	} else {
		uvID := userView.Persist(ctx, uvPath, result.NewProjectKBID, msg.InstallationID, msg.RepositoryID, msg.ProjectID, version, cfg.AWSS3Bucket, "", "")
		if uvID != 0 {
			result.UserViewReportID = &uvID
			userViewStep.Complete(slog.Int64("userViewReportId", uvID))
		} else {
			userViewStep.Fail(fmt.Errorf("user view persist returned 0"))
		}
	}

	// 11. touch 파일 업데이트
	touchStep := logger.StepStart(ctx, "workspace.touch", jobID)
	if _, err := disk.CreateTouchFileAtomic(ctx, localPath); err != nil {
		touchStep.Fail(err)
		logger.Warn(ctx, "failed to update touch file", slog.String("reason", err.Error()))
	} else {
		touchStep.Complete()
	}

	// 12. 작업 완료: ANALYSIS_JOB_COMPLETED
	statusStep := logger.StepStart(ctx, "job.complete", jobID)
	if err := db.UpdateAnalysisJobStatus(jobIDInt, "ANALYSIS_JOB_COMPLETED"); err != nil {
		statusStep.Fail(err)
		logger.Warn(ctx, "failed to update analysis job status to completed", slog.String("reason", err.Error()))
	} else {
		statusStep.Complete()
	}

	logger.AnalysisCompleted(ctx, jobID, time.Since(startAt).Milliseconds(),
		slog.String("analysisType", "FULL_SCAN"),
		slog.String("repo", msg.RepositoryFullName),
	)
	return result, nil
}
