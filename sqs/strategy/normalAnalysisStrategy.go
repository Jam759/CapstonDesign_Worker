package strategy

import (
	"context"
	"encoding/json"
	"log"
	"path/filepath"
	"strconv"
	"worker_GoVer/apperrors"
	"worker_GoVer/artifact/codeGraph"
	"worker_GoVer/artifact/projectContext"
	"worker_GoVer/artifact/userView"
	"worker_GoVer/config"
	"worker_GoVer/db"
	"worker_GoVer/disk"
	"worker_GoVer/git"
	"worker_GoVer/quest"
	"worker_GoVer/s3"
)

func (s NormalAnalysisStrategy) Handle(ctx context.Context, jobID string, data json.RawMessage) (*StrategyResult, error) {
	var msg NormalAnalysisQueueMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, apperrors.Newf(apperrors.ErrUnmarshalMessage, 400, false, err, "failed to unmarshal NormalAnalysis message")
	}

	log.Printf("[NormalAnalysis] start jobId=%s repo=%s branch=%s isMerge=%v",
		jobID, msg.RepositoryFullName, msg.BranchName, msg.IsMerge)

	cfg := config.Get()
	jobIDInt := msg.JobID
	if jobIDInt == 0 {
		jobIDInt, _ = strconv.ParseInt(jobID, 10, 64)
	}

	// 1. job 선점: ANALYSIS_JOB_QUEUED → ANALYSIS_JOB_RUNNING
	claimed, err := db.ClaimAnalysisJob(jobIDInt)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "failed to claim job jobId=%s", jobID)
	}
	if !claimed {
		log.Printf("[NormalAnalysis] job already claimed or not in QUEUED state, skipping jobId=%s", jobID)
		return nil, nil
	}

	// 2. 디스크 정리
	if err := disk.IfNeedDoCleanWorkspace(); err != nil {
		log.Printf("[NormalAnalysis] workspace cleanup warning: %v", err)
	}

	// 3. 로컬 경로 결정: WorkspaceBaseDir/{installationId}/{repoId}
	localPath := filepath.Join(cfg.WorkspaceBaseDir,
		strconv.FormatInt(msg.PushUserInstallationID, 10),
		strconv.FormatInt(msg.RepositoryID, 10),
	)

	// 4. clone or fetch
	exists, err := disk.IsExistDir(localPath)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to check dir")
	}
	if exists {
		if err := git.Fetch(localPath, msg.PushUserInstallationID); err != nil {
			return nil, apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to fetch repo=%s", msg.RepositoryFullName)
		}
	} else {
		if err := git.CloneRepository(msg.PushUserInstallationID, msg.RepositoryFullName, localPath, msg.BranchName); err != nil {
			return nil, apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to clone repo=%s branch=%s", msg.RepositoryFullName, msg.BranchName)
		}
	}

	// 5. 브랜치 체크아웃
	if err := git.CheckoutBranch(localPath, msg.BranchName); err != nil {
		return nil, apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to checkout branch=%s", msg.BranchName)
	}

	// 6. afterCommit checkout
	if err := git.Checkout(localPath, msg.AfterCommit); err != nil {
		return nil, apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to checkout afterCommit=%s", msg.AfterCommit)
	}

	// 7. lock
	if locked, _ := disk.IsLocked(localPath); locked {
		return nil, apperrors.Newf(apperrors.ErrWorkspaceLocked, 409, true, nil, "repository is locked jobId=%s", jobID)
	}
	if _, err := disk.CreateLockFileAtomic(localPath); err != nil {
		return nil, apperrors.Newf(apperrors.ErrWorkspaceLocked, 500, true, err, "failed to acquire lock jobId=%s", jobID)
	}
	defer func() {
		if err := disk.RemoveLockAtomic(localPath); err != nil {
			log.Printf("[NormalAnalysis] failed to remove lock: %v", err)
		}
	}()

	// 7. 최신 PROJECT_KB 조회
	latestKB, err := db.GetLatestProjectKBReport(msg.ProjectID)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrDBOperation, 500, true, err, "failed to get latest PROJECT_KB projectId=%d", msg.ProjectID)
	}
	if latestKB == nil {
		return nil, apperrors.Newf(apperrors.ErrNoProjectKB, 422, false, nil, "no PROJECT_KB found for projectId=%d", msg.ProjectID)
	}

	// 8. effectiveBeforeCommit 결정
	// 최신 KB의 afterCommitHash가 다르면 그걸 기준으로 삼는다
	effectiveBeforeCommit := msg.BeforeCommit
	if latestKB.AfterCommitHash != "" && latestKB.AfterCommitHash != msg.BeforeCommit {
		effectiveBeforeCommit = latestKB.AfterCommitHash
	}

	// 9. baseline PROJECT_KB를 S3에서 다운로드
	artifactDir := filepath.Join(localPath, "artifact")
	baselinePath, err := s3.DownloadProjectKB(latestKB.S3Bucket, latestKB.StoredURL, artifactDir)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrS3Operation, 500, true, err, "failed to download baseline KB projectId=%d", msg.ProjectID)
	}

	// 10. git diff 생성
	diffPath, err := git.Diff(localPath, effectiveBeforeCommit, msg.AfterCommit, msg.IsMerge)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to generate diff before=%s after=%s", effectiveBeforeCommit, msg.AfterCommit)
	}

	// 11. 변경 파일 목록
	diffFiles, err := git.DiffFileList(localPath, effectiveBeforeCommit, msg.AfterCommit, msg.IsMerge)
	if err != nil {
		log.Printf("[NormalAnalysis] failed to get diff file list (non-fatal): %v", err)
	}
	changedPaths := make([]string, 0, len(diffFiles))
	for _, f := range diffFiles {
		changedPaths = append(changedPaths, f.Path)
		if f.PreviousPath != "" {
			changedPaths = append(changedPaths, f.PreviousPath)
		}
	}

	// 12. CodeGraph 생성
	graphPath, err := codeGraph.GenerateCodeGraph(localPath)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to generate code graph repo=%s", msg.RepositoryFullName)
	}

	// 13. incremental ProjectContext 업데이트
	kbVersion, err := db.NextReportVersion(msg.ProjectID, "PROJECT_KB")
	if err != nil {
		log.Printf("[NormalAnalysis] failed to get KB version (non-fatal): %v", err)
		kbVersion = 1
	}
	ctxPath, err := projectContext.UpdateProjectContext(
		localPath, baselinePath, diffPath, graphPath, changedPaths, kbVersion,
	)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to update project context")
	}

	// 14. ProjectKB S3 업로드 + DB 저장
	prevKBID := int64(latestKB.ProjectAnalysisReportsID)
	newKBID := projectContext.Persist(ctxPath, &prevKBID, msg.PushUserInstallationID, msg.RepositoryID, msg.ProjectID, kbVersion, cfg.AWSS3Bucket, effectiveBeforeCommit, msg.AfterCommit)

	result := &StrategyResult{}
	if newKBID != 0 {
		result.NewProjectKBID = &newKBID
	}

	// 15. Quest 평가 및 생성
	questReq, err := quest.BuildQuestRequest(jobIDInt, msg.ProjectID, msg.PushUserID, msg.RepositoryFullName, msg.BranchName)
	if err != nil {
		log.Printf("[NormalAnalysis] failed to build quest request (non-fatal): %v", err)
	} else {
		questResp, err := quest.GenerateAndEvaluateQuests(ctxPath, questReq)
		if err != nil {
			log.Printf("[NormalAnalysis] failed to generate quests (non-fatal): %v", err)
		} else {
			result.CompleteQuestIDs, result.NewQuestIDs = quest.SaveResults(jobIDInt, msg.ProjectID, msg.PushUserID, questResp)

			// 16. UserView 생성
			uvVersion, err := db.NextReportVersion(msg.ProjectID, "USER_VIEW")
			if err != nil {
				log.Printf("[NormalAnalysis] failed to get user view version (non-fatal): %v", err)
				uvVersion = 1
			}
			uvInput := userView.GenerateInput{
				ProjectID:          msg.ProjectID,
				UserID:             msg.PushUserID,
				RepositoryFullName: msg.RepositoryFullName,
				BranchName:         msg.BranchName,
				BeforeCommitHash:   effectiveBeforeCommit,
				AfterCommitHash:    msg.AfterCommit,
				Version:            uvVersion,
				CompletedQuestIDs:  result.CompleteQuestIDs,
				NewQuestIDs:        result.NewQuestIDs,
			}
			if uvPath, err := userView.Generate(uvInput, ctxPath, localPath); err != nil {
				log.Printf("[NormalAnalysis] failed to generate user view (non-fatal): %v", err)
			} else {
				uvID := userView.Persist(uvPath, result.NewProjectKBID, msg.PushUserInstallationID, msg.RepositoryID, msg.ProjectID, uvVersion, cfg.AWSS3Bucket, effectiveBeforeCommit, msg.AfterCommit)
				if uvID != 0 {
					result.UserViewReportID = &uvID
				}
			}
		}
	}

	// 17. touch 파일 업데이트
	if _, err := disk.CreateTouchFileAtomic(localPath); err != nil {
		log.Printf("[NormalAnalysis] failed to touch: %v", err)
	}

	// 18. 작업 완료: ANALYSIS_JOB_COMPLETED
	if err := db.UpdateAnalysisJobStatus(jobIDInt, "ANALYSIS_JOB_COMPLETED"); err != nil {
		log.Printf("[NormalAnalysis] failed to update job status to COMPLETED: %v", err)
	}

	log.Printf("[NormalAnalysis] done jobId=%s repo=%s", jobID, msg.RepositoryFullName)
	return result, nil
}
