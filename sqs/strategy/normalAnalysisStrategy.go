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
	"worker_GoVer/artifact/roadmap"
	"worker_GoVer/artifact/userView"
	"worker_GoVer/config"
	"worker_GoVer/db"
	"worker_GoVer/disk"
	"worker_GoVer/git"
	"worker_GoVer/keyword"
	"worker_GoVer/quest"
	"worker_GoVer/s3"
)

func (s NormalAnalysisStrategy) Handle(ctx context.Context, base SqsBaseMessage) (*StrategyResult, error) {
	startAt := time.Now()

	dataBytes, err := json.Marshal(base.Data)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrUnmarshalMessage, 400, false, err, "failed to marshal NormalAnalysis data")
	}
	var msg NormalAnalysisQueueMessage
	if err := json.Unmarshal(dataBytes, &msg); err != nil {
		return nil, apperrors.Newf(apperrors.ErrUnmarshalMessage, 400, false, err, "failed to unmarshal NormalAnalysis message")
	}

	jobID := base.JobID
	log.AnalysisStarted(ctx, jobID,
		slog.String("analysisType", "NORMAL_ANALYSIS_REQUEST"),
		slog.String("repo", msg.RepositoryFullName),
		slog.String("branch", msg.BranchName),
		slog.Bool("isMerge", msg.IsMerge),
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
		log.AnalysisFailed(ctx, jobID, err, time.Since(startAt).Milliseconds(),
			slog.String("analysisType", "NORMAL_ANALYSIS_REQUEST"),
			slog.String("repo", msg.RepositoryFullName),
		)
		rb.Run(ctx)
		if jobClaimed {
			if dbErr := db.UpdateAnalysisJobStatus(jobIDInt, "NOTIFICATION_QUEUED"); dbErr != nil {
				log.Warn(ctx, "rollback: failed to mark job as NOTIFICATION_QUEUED", dbErr)
			}
		}
		return err
	}

	// 1. job 선점: ANALYSIS_JOB_QUEUED → ANALYSIS_JOB_RUNNING
	claimStep := log.StepStart(ctx, "job.claim", jobID)
	ok, err := db.ClaimAnalysisJob(jobIDInt)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "failed to claim job jobId=%s", jobID)
		claimStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	if !ok {
		claimStep.Complete(slog.String("status", "skipped"))
		log.Warn(ctx, "analysis job already claimed, skipping", nil)
		return nil, nil
	}
	claimStep.Complete()
	jobClaimed = true

	// 2. 디스크 정리
	cleanupStep := log.StepStart(ctx, "workspace.cleanup", jobID)
	if err := disk.IfNeedDoCleanWorkspace(ctx); err != nil {
		cleanupStep.Fail(err)
		log.Warn(ctx, "workspace cleanup warning", err)
	} else {
		cleanupStep.Complete()
	}

	// 3. 로컬 경로 결정
	localPath := filepath.Join(cfg.WorkspaceBaseDir,
		strconv.FormatInt(msg.PushUserInstallationID, 10),
		strconv.FormatInt(msg.RepositoryID, 10),
	)

	// 4. clone or fetch
	repoStep := log.StepStart(ctx, "git.prepare", jobID, slog.String("localPath", localPath))
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
	checkoutBranchStep := log.StepStart(ctx, "git.checkout_branch", jobID, slog.String("branch", msg.BranchName))
	if err := git.CheckoutBranch(ctx, localPath, msg.BranchName); err != nil {
		wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to checkout branch=%s", msg.BranchName)
		checkoutBranchStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	checkoutBranchStep.Complete()

	// 6. afterCommit checkout
	checkoutCommitStep := log.StepStart(ctx, "git.checkout_commit", jobID, slog.String("afterCommit", msg.AfterCommit))
	if err := git.Checkout(ctx, localPath, msg.AfterCommit); err != nil {
		wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to checkout afterCommit=%s", msg.AfterCommit)
		checkoutCommitStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	checkoutCommitStep.Complete()

	// 7. lock
	lockStep := log.StepStart(ctx, "workspace.lock", jobID)
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
			log.Warn(ctx, "failed to remove workspace lock", err)
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
	downloadStep := log.StepStart(ctx, "project_context.download", jobID)
	baselinePath, err := s3.DownloadProjectKB(ctx, latestKB.S3Bucket, latestKB.StoredURL, artifactDir)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrS3Operation, 500, true, err, "failed to download baseline KB projectId=%d", msg.ProjectID)
		downloadStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	downloadStep.Complete()

	// 10. git diff 생성
	diffStep := log.StepStart(ctx, "git.diff", jobID)
	diffPath, err := git.Diff(ctx, localPath, baseCommit, msg.AfterCommit, msg.IsMerge)
	if err != nil {
		wrapped := apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to generate diff before=%s after=%s", baseCommit, msg.AfterCommit)
		diffStep.Fail(wrapped)
		return nil, fail(wrapped)
	}
	diffStep.Complete()

	// 11. 변경 파일 목록
	diffFileStep := log.StepStart(ctx, "git.diff_files", jobID)
	diffFiles, err := git.DiffFileList(ctx, localPath, baseCommit, msg.AfterCommit, msg.IsMerge)
	if err != nil {
		diffFileStep.Fail(err)
		log.Warn(ctx, "failed to get diff file list", err)
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
	graphStep := log.StepStart(ctx, "codegraph.generate", jobID)
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
		log.Warn(ctx, "failed to get project KB version, defaulting to 1", err)
		kbVersion = 1
	}
	updateContextStep := log.StepStart(ctx, "project_context.update", jobID, slog.Int("version", kbVersion))
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
		projectMeta,
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
	persistStep := log.StepStart(ctx, "project_context.persist", jobID, slog.Int("version", kbVersion))
	newKBID, newKBURL, err := projectContext.Persist(ctx, ctxPath, &previousKBID, msg.PushUserInstallationID, msg.RepositoryID, msg.ProjectID, kbVersion, cfg.AWSS3Bucket, baseCommit, msg.AfterCommit)
	if err != nil {
		persistStep.Fail(err)
		if newKBURL != "" {
			if delErr := s3.DeleteObject(ctx, cfg.AWSS3Bucket, newKBURL); delErr != nil {
				log.Warn(ctx, "rollback: failed to delete orphaned PROJECT_KB from S3", delErr)
			}
		}
		return nil, fail(apperrors.Newf(apperrors.ErrS3Operation, 500, true, err, "failed to persist project context"))
	}
	result.NewProjectKBID = &newKBID
	persistStep.Complete(slog.Int64("projectKbId", newKBID))
	rb.Add(func() {
		if delErr := s3.DeleteObject(ctx, cfg.AWSS3Bucket, newKBURL); delErr != nil {
			log.Warn(ctx, "rollback: failed to delete PROJECT_KB from S3", delErr)
		}
		if delErr := db.DeleteAnalysisReport(newKBID); delErr != nil {
			log.Warn(ctx, "rollback: failed to delete PROJECT_KB report from DB", delErr)
		}
	})

	// 15. Quest 평가 및 생성
	questStep := log.StepStart(ctx, "quest.generate", jobID)
	questReq, err := quest.BuildQuestRequest(ctx, jobIDInt, msg.ProjectID, base.UserID, msg.ProjectTitle, msg.ProjectDescription, msg.ProjectGoal, msg.RepositoryFullName, msg.BranchName)
	if err != nil {
		questStep.Fail(err)
		return nil, fail(apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "failed to build quest request"))
	}
	questResp, err := quest.GenerateAndEvaluateQuests(ctx, ctxPath, questReq)
	if err != nil {
		questStep.Fail(err)
		return nil, fail(apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to generate quests"))
	}
	result.CompleteQuestIDs, result.NewQuestIDs, _ = quest.SaveResults(ctx, jobIDInt, msg.ProjectID, base.UserID, questReq, questResp)
	rb.Add(func() {
		if delErr := db.DeleteQuestEvaluationsByJobID(jobIDInt); delErr != nil {
			log.Warn(ctx, "rollback: failed to delete quest evaluations", delErr)
		}
		if delErr := db.DeleteQuestsByIDs(result.NewQuestIDs); delErr != nil {
			log.Warn(ctx, "rollback: failed to delete new quests", delErr)
		}
		if delErr := db.RevertQuestCompletion(result.CompleteQuestIDs); delErr != nil {
			log.Warn(ctx, "rollback: failed to revert quest completion", delErr)
		}
	})
	questStep.Complete(
		slog.Int("completedQuestCount", len(result.CompleteQuestIDs)),
		slog.Int("newQuestCount", len(result.NewQuestIDs)),
	)

	// 16. 마일스톤 진행 평가
	milestoneEvalStep := log.StepStart(ctx, "roadmap.milestone_eval", jobID)
	milestoneReq, err := roadmap.BuildMilestoneEvalRequest(ctx, jobIDInt, msg.ProjectID, msg.ProjectTitle, msg.ProjectDescription, msg.ProjectGoal)
	if err != nil {
		milestoneEvalStep.Fail(err)
		log.Warn(ctx, "failed to build milestone eval request", err)
	} else if milestoneReq == nil {
		milestoneEvalStep.Complete(slog.String("status", "no_active_milestones"))
	} else {
		milestoneResp, err := roadmap.EvaluateMilestones(ctx, milestoneReq, ctxPath, diffPath)
		if err != nil {
			milestoneEvalStep.Fail(err)
			log.Warn(ctx, "milestone evaluation failed", err)
		} else {
			changedIDs, prevStatuses := roadmap.SaveMilestoneEvalResults(ctx, jobIDInt, milestoneReq, milestoneResp)
			milestoneEvalStep.Complete(
				slog.Int("evaluated", len(milestoneResp.MilestoneEvaluations)),
				slog.Int("statusUpdates", len(changedIDs)),
			)
			if len(changedIDs) > 0 {
				rb.Add(func() {
					if delErr := db.DeleteMilestoneEvaluationsByJobID(jobIDInt); delErr != nil {
						log.Warn(ctx, "rollback: failed to delete milestone evaluations", delErr)
					}
					if revertErr := db.RevertMilestoneStatuses(prevStatuses); revertErr != nil {
						log.Warn(ctx, "rollback: failed to revert milestone statuses", revertErr)
					}
				})
			}
		}
	}

	// 17. UserView 생성
	uvVersion, err := db.NextReportVersion(msg.ProjectID, "USER_VIEW")
	if err != nil {
		log.Warn(ctx, "failed to get user view version, defaulting to 1", err)
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
		BeforeCommitHash:   baseCommit,
		AfterCommitHash:    msg.AfterCommit,
		Version:            uvVersion,
		CompletedQuestIDs:  result.CompleteQuestIDs,
		NewQuestIDs:        result.NewQuestIDs,
	}
	userViewStep := log.StepStart(ctx, "user_view.generate", jobID, slog.Int("version", uvVersion))
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
				log.Warn(ctx, "rollback: failed to delete orphaned USER_VIEW from S3", delErr)
			}
		}
		return nil, fail(apperrors.Newf(apperrors.ErrS3Operation, 500, true, err, "failed to persist user view"))
	}
	result.UserViewReportID = &uvID
	userViewStep.Complete(slog.Int64("userViewReportId", uvID))
	rb.Add(func() {
		if delErr := s3.DeleteObject(ctx, cfg.AWSS3Bucket, uvURL); delErr != nil {
			log.Warn(ctx, "rollback: failed to delete USER_VIEW from S3", delErr)
		}
		if delErr := db.DeleteAnalysisReport(uvID); delErr != nil {
			log.Warn(ctx, "rollback: failed to delete USER_VIEW report from DB", delErr)
		}
	})

	// 18. 검색 키워드 추출
	kwStep := log.StepStart(ctx, "keyword.extract", jobID)
	if err := keyword.ExtractAndSave(ctx, jobIDInt, msg.ProjectID, ctxPath); err != nil {
		kwStep.Fail(err)
		log.Warn(ctx, "keyword extraction failed, skipping", err)
	} else {
		kwStep.Complete()
	}

	// 20. touch 파일 업데이트
	touchStep := log.StepStart(ctx, "workspace.touch", jobID)
	if _, err := disk.CreateTouchFileAtomic(ctx, localPath); err != nil {
		touchStep.Fail(err)
		log.Warn(ctx, "failed to update touch file", err)
	} else {
		touchStep.Complete()
	}

	// 21. 작업 완료
	statusStep := log.StepStart(ctx, "job.complete", jobID)
	if err := db.UpdateAnalysisJobStatus(jobIDInt, "ANALYSIS_JOB_COMPLETED"); err != nil {
		statusStep.Fail(err)
		log.Warn(ctx, "failed to update analysis job status to completed", err)
	} else {
		statusStep.Complete()
	}

	log.AnalysisCompleted(ctx, jobID, time.Since(startAt).Milliseconds(),
		slog.String("analysisType", "NORMAL_ANALYSIS_REQUEST"),
		slog.String("repo", msg.RepositoryFullName),
	)
	return result, nil
}
