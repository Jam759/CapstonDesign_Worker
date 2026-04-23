package roadmap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"worker_GoVer/ai"
	"worker_GoVer/db"
	"worker_GoVer/logger"
)

type MilestoneEvalRequest struct {
	JobID      int64          `json:"jobId"`
	Project    evalProject    `json:"project"`
	Milestones []milestoneItem `json:"milestones"`
}

type evalProject struct {
	ProjectID          int64  `json:"projectId"`
	ProjectTitle       string `json:"projectTitle"`
	ProjectDescription string `json:"projectDescription"`
	ProjectGoal        string `json:"projectGoal"`
}

type milestoneItem struct {
	ProjectMilestoneID int64  `json:"projectMilestoneId"`
	PhaseName          string `json:"phaseName"`
	MilestoneName      string `json:"milestoneName"`
	MilestoneIntent    string `json:"milestoneIntent"`
	TriggerCondition   string `json:"triggerCondition"`
	ExpectedState      string `json:"expectedState"`
	CompletionRule     string `json:"completionRule"`
	Status             string `json:"status"`
}

type MilestoneEvalResponse struct {
	MilestoneEvaluations []MilestoneEvalResult `json:"milestoneEvaluations"`
}

type MilestoneEvalResult struct {
	ProjectMilestoneID int64   `json:"projectMilestoneId"`
	EvaluationResult   string  `json:"evaluationResult"`
	ConfidenceScore    float64 `json:"confidenceScore"`
	Reason             string  `json:"reason"`
	ProgressNote       string  `json:"progressNote"`
}

// BuildMilestoneEvalRequest는 DB에서 PENDING/IN_PROGRESS 마일스톤을 조회해 평가 요청을 조립합니다.
func BuildMilestoneEvalRequest(ctx context.Context, jobID int64, projectID int64, projectTitle, projectDescription, projectGoal string) (*MilestoneEvalRequest, error) {
	rows, err := db.FetchActiveMilestones(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch active milestones: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	items := make([]milestoneItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, milestoneItem{
			ProjectMilestoneID: r.ProjectMilestoneID,
			PhaseName:          r.PhaseName,
			MilestoneName:      r.MilestoneName,
			MilestoneIntent:    r.MilestoneIntent,
			TriggerCondition:   r.TriggerCondition,
			ExpectedState:      r.ExpectedState,
			CompletionRule:     r.CompletionRule,
			Status:             r.Status,
		})
	}

	return &MilestoneEvalRequest{
		JobID: jobID,
		Project: evalProject{
			ProjectID:          projectID,
			ProjectTitle:       projectTitle,
			ProjectDescription: projectDescription,
			ProjectGoal:        projectGoal,
		},
		Milestones: items,
	}, nil
}

// EvaluateMilestones는 AI를 호출해 마일스톤 진행 여부를 평가합니다.
func EvaluateMilestones(ctx context.Context, request *MilestoneEvalRequest, projectContextPath, diffPath string) (*MilestoneEvalResponse, error) {
	requestJSON, _ := json.Marshal(request)

	p := ai.MilestoneEvalPrompt(string(requestJSON))
	logger.Info(ctx, "milestone evaluation start", slog.Int("milestoneCount", len(request.Milestones)))

	files := []string{projectContextPath}
	if diffPath != "" {
		files = append(files, diffPath)
	}
	result := <-ai.GenerateMessageWithFiles(ctx, p.User, p.System, files)
	if result.Err != nil {
		return nil, fmt.Errorf("AI milestone evaluation failed: %w", result.Err)
	}

	responseStr, ok := result.Data.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected AI response type")
	}
	responseStr = cleanTrailingCommas(extractJSONObject(responseStr))

	var response MilestoneEvalResponse
	if err := json.Unmarshal([]byte(responseStr), &response); err != nil {
		return nil, fmt.Errorf("failed to parse milestone eval response: %w", err)
	}

	logger.Info(ctx, "milestone evaluation completed", slog.Int("evaluations", len(response.MilestoneEvaluations)))
	return &response, nil
}

// SaveMilestoneEvalResults는 평가 결과를 DB에 저장하고 상태를 업데이트합니다.
// 반환값: 상태가 변경된 milestoneID 목록, 롤백용 이전 상태 맵
func SaveMilestoneEvalResults(ctx context.Context, jobID int64, request *MilestoneEvalRequest, response *MilestoneEvalResponse) (changedIDs []int64, prevStatuses map[int64]string) {
	changedIDs = make([]int64, 0)
	prevStatuses = make(map[int64]string)

	currentStatusByID := make(map[int64]string, len(request.Milestones))
	for _, m := range request.Milestones {
		currentStatusByID[m.ProjectMilestoneID] = m.Status
	}

	for _, eval := range response.MilestoneEvaluations {
		currentStatus, known := currentStatusByID[eval.ProjectMilestoneID]
		if !known {
			logger.Warn(ctx, "skipping evaluation for unknown milestone",
				slog.Int64("milestoneId", eval.ProjectMilestoneID),
			)
			continue
		}

		newStatus := normalizeMilestoneStatus(eval.EvaluationResult)

		if err := db.InsertMilestoneEvaluation(eval.ProjectMilestoneID, jobID, newStatus, eval.ConfidenceScore, eval.Reason, eval.ProgressNote); err != nil {
			logger.Error(ctx, "failed to insert milestone evaluation", err, slog.Int64("milestoneId", eval.ProjectMilestoneID))
			continue
		}

		if shouldUpgrade(currentStatus, newStatus) {
			if err := db.UpdateMilestoneStatus(eval.ProjectMilestoneID, newStatus); err != nil {
				logger.Error(ctx, "failed to update milestone status", err, slog.Int64("milestoneId", eval.ProjectMilestoneID))
				continue
			}
			prevStatuses[eval.ProjectMilestoneID] = currentStatus
			changedIDs = append(changedIDs, eval.ProjectMilestoneID)
		}
	}

	logger.Info(ctx, "milestone eval results saved",
		slog.Int("evaluations", len(response.MilestoneEvaluations)),
		slog.Int("statusUpdates", len(changedIDs)),
	)
	return
}

// shouldUpgrade는 상태가 앞으로 진행되는 경우만 true를 반환합니다 (다운그레이드 방지).
func shouldUpgrade(current, next string) bool {
	rank := map[string]int{"PENDING": 0, "IN_PROGRESS": 1, "ACHIEVED": 2, "FAILED": 3}
	return rank[next] > rank[current]
}

