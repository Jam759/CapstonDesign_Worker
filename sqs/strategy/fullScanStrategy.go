package strategy

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
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
	"worker_GoVer/quest"
)

func (s FullScanStrategy) Handle(_ context.Context, jobID string, data json.RawMessage) (*StrategyResult, error) {
	var msg FullScanQueueMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, apperrors.Newf(apperrors.ErrUnmarshalMessage, 400, false, err, "failed to unmarshal FullScan message")
	}

	log.Printf("[FullScan] start jobId=%s repo=%s branch=%s installationId=%d repoId=%d projectId=%d",
		jobID, msg.RepositoryFullName, msg.BranchName,
		msg.InstallationID, msg.RepositoryID, msg.ProjectID,
	)

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
		log.Printf("[FullScan] job already claimed or not in QUEUED state, skipping jobId=%s", jobID)
		return nil, nil
	}

	// 2. 디스크 정리
	if err := disk.IfNeedDoCleanWorkspace(); err != nil {
		log.Printf("[FullScan] workspace cleanup warning: %v", err)
	}

	// 3. 로컬 경로 결정: WorkspaceBaseDir/{installationId}/{repoId}
	localPath := filepath.Join(cfg.WorkspaceBaseDir,
		strconv.FormatInt(msg.InstallationID, 10),
		strconv.FormatInt(msg.RepositoryID, 10),
	)

	// 4. clone or fetch
	exists, err := disk.IsExistDir(localPath)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to check dir")
	}
	if exists {
		if err := git.Fetch(localPath, msg.InstallationID); err != nil {
			return nil, apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to fetch repo=%s", msg.RepositoryFullName)
		}
	} else {
		if err := git.CloneRepository(msg.InstallationID, msg.RepositoryFullName, localPath, msg.BranchName); err != nil {
			return nil, apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to clone repo=%s branch=%s", msg.RepositoryFullName, msg.BranchName)
		}
	}

	// 5. 브랜치 체크아웃
	if err := git.CheckoutBranch(localPath, msg.BranchName); err != nil {
		return nil, apperrors.Newf(apperrors.ErrGitOperation, 500, true, err, "failed to checkout branch=%s", msg.BranchName)
	}

	// 6. lock
	if locked, _ := disk.IsLocked(localPath); locked {
		return nil, apperrors.Newf(apperrors.ErrWorkspaceLocked, 409, true, nil, "repository is locked jobId=%s", jobID)
	}
	if _, err := disk.CreateLockFileAtomic(localPath); err != nil {
		return nil, apperrors.Newf(apperrors.ErrWorkspaceLocked, 500, true, err, "failed to acquire lock jobId=%s", jobID)
	}
	defer func() {
		if err := disk.RemoveLockAtomic(localPath); err != nil {
			log.Printf("[FullScan] failed to remove lock: %v", err)
		}
	}()

	// 6. CodeGraph 생성
	graphPath, err := codeGraph.GenerateCodeGraph(localPath)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to generate code graph repo=%s", msg.RepositoryFullName)
	}

	// 7. CodeContent 생성
	graphData, err := os.ReadFile(graphPath)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to read code graph")
	}
	var graph codeGraphStrategy.CodeGraph
	if err := json.Unmarshal(graphData, &graph); err != nil {
		return nil, apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to parse code graph")
	}
	contentPath, err := codeContent.GenerateCodeContent(localPath, &graph)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrCodeGraphGeneration, 500, true, err, "failed to generate code content")
	}

	// 8. ProjectContext 생성 (AI 분석, 파일로 전달)
	ctxVersion, err := db.NextReportVersion(msg.ProjectID, "PROJECT_KB")
	if err != nil {
		log.Printf("[FullScan] failed to get project context version (non-fatal): %v", err)
		ctxVersion = 1
	}
	ctxPath, err := projectContext.GenerateProjectContext(localPath, graphPath, contentPath, ctxVersion)
	if err != nil {
		return nil, apperrors.Newf(apperrors.ErrAIAnalysis, 500, true, err, "failed to generate project context")
	}

	// 8-1. ProjectContext S3 업로드 + DB 저장
	newKBID := projectContext.Persist(ctxPath, nil, msg.InstallationID, msg.RepositoryID, msg.ProjectID, ctxVersion, cfg.AWSS3Bucket, "", "")

	result := &StrategyResult{}
	if newKBID != 0 {
		result.NewProjectKBID = &newKBID
	}

	// 9. Quest 평가 및 생성
	questReq, err := quest.BuildQuestRequest(jobIDInt, msg.ProjectID, msg.UserID, msg.RepositoryFullName, msg.BranchName)
	if err != nil {
		log.Printf("[FullScan] failed to build quest request (non-fatal): %v", err)
	} else {
		questResp, err := quest.GenerateAndEvaluateQuests(ctxPath, questReq)
		if err != nil {
			log.Printf("[FullScan] failed to generate quests (non-fatal): %v", err)
		} else {
			result.CompleteQuestIDs, result.NewQuestIDs = quest.SaveResults(jobIDInt, msg.ProjectID, msg.UserID, questResp)
		}
	}

	// 10. UserView 생성
	version, err := db.NextReportVersion(msg.ProjectID, "USER_VIEW")
	if err != nil {
		log.Printf("[FullScan] failed to get user view version (non-fatal): %v", err)
		version = 1
	}
	uvInput := userView.GenerateInput{
		ProjectID:          msg.ProjectID,
		UserID:             msg.UserID,
		RepositoryFullName: msg.RepositoryFullName,
		BranchName:         msg.BranchName,
		Version:            version,
	}
	if uvPath, err := userView.Generate(uvInput, ctxPath, localPath); err != nil {
		log.Printf("[FullScan] failed to generate user view (non-fatal): %v", err)
	} else {
		uvID := userView.Persist(uvPath, result.NewProjectKBID, msg.InstallationID, msg.RepositoryID, msg.ProjectID, version, cfg.AWSS3Bucket, "", "")
		if uvID != 0 {
			result.UserViewReportID = &uvID
		}
	}

	// 11. touch 파일 업데이트
	if _, err := disk.CreateTouchFileAtomic(localPath); err != nil {
		log.Printf("[FullScan] failed to touch: %v", err)
	}

	// 12. 작업 완료: ANALYSIS_JOB_COMPLETED
	if err := db.UpdateAnalysisJobStatus(jobIDInt, "ANALYSIS_JOB_COMPLETED"); err != nil {
		log.Printf("[FullScan] failed to update job status to COMPLETED: %v", err)
	}

	log.Printf("[FullScan] done jobId=%s repo=%s", jobID, msg.RepositoryFullName)
	return result, nil
}
