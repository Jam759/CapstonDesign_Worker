package quest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"worker_GoVer/ai"
	"worker_GoVer/db"
	"worker_GoVer/logger"
)

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i != -1 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

// BuildQuestRequest는 DB에서 기존 퀘스트와 최근 평가 이력을 조회하여 QuestRequest를 조립합니다.
func BuildQuestRequest(ctx context.Context, jobID int64, projectID int64, userID int64, projectTitle string, projectDescription string, projectGoal string, repositoryFullName string, branchName string) (QuestRequest, error) {
	_ = ctx
	// 1. ACTIVE 퀘스트 조회
	rows, err := db.FetchActiveQuests(projectID, userID)
	if err != nil {
		return QuestRequest{}, fmt.Errorf("failed to fetch quests: %w", err)
	}

	questIDs := make([]int64, 0, len(rows))
	quests := make([]UserQuest, 0, len(rows))
	for _, r := range rows {
		questIDs = append(questIDs, r.UserAiQuestID)
		lastEvaluatedAt := ""
		if r.LastEvaluatedAt != nil {
			lastEvaluatedAt = r.LastEvaluatedAt.Format("2006-01-02T15:04:05")
		}
		quests = append(quests, UserQuest{
			UserAiQuestID:      r.UserAiQuestID,
			Title:              r.Title,
			Description:        r.Description,
			Hint:               r.Hint,
			AIGenerationReason: r.AIGenerationReason,
			CompletionGuide:    r.CompletionGuide,
			ApprovalStatus:     r.ApprovalStatus,
			ProgressStatus:     r.ProgressStatus,
			LastEvaluatedAt:    lastEvaluatedAt,
		})
	}

	// 2. 최근 평가 이력 조회
	evalRows, err := db.FetchRecentEvaluations(questIDs)
	if err != nil {
		return QuestRequest{}, fmt.Errorf("failed to fetch evaluations: %w", err)
	}

	evaluations := make([]QuestEvaluation, 0, len(evalRows))
	for _, e := range evalRows {
		evaluations = append(evaluations, QuestEvaluation{
			UserAiQuestID:    e.UserAiQuestID,
			EvaluationResult: e.EvaluationResult,
			ConfidenceScore:  e.ConfidenceScore,
			Reason:           e.Reason,
			ProgressNote:     e.ProgressNote,
			EvaluatedAt:      e.EvaluatedAt.Format("2006-01-02T15:04:05"),
		})
	}

	return QuestRequest{
		JobID: jobID,
		Project: Project{
			ProjectID:          projectID,
			UserID:             userID,
			ProjectTitle:       projectTitle,
			ProjectDescription: projectDescription,
			ProjectGoal:        projectGoal,
			RepositoryFullName: repositoryFullName,
			BranchName:         branchName,
		},
		Quests:                     quests,
		MostRecentQuestEvaluations: evaluations,
	}, nil
}

// GenerateAndEvaluateQuests는 projectContext 파일 기반으로 퀘스트를 평가하고 새 퀘스트를 생성합니다.
func GenerateAndEvaluateQuests(ctx context.Context, projCtxPath string, request QuestRequest) (*QuestResponse, error) {
	requestJSON, _ := json.Marshal(request)

	p := ai.QuestPrompt(string(requestJSON))
	logger.Info(ctx, "quest generation start", slog.Int("questCount", len(request.Quests)))
	result := <-ai.GenerateMessageWithFiles(ctx, p.User, p.System, []string{projCtxPath})
	if result.Err != nil {
		return nil, fmt.Errorf("failed to generate quests: %w", result.Err)
	}

	responseStr, ok := result.Data.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected response type")
	}
	responseStr = extractJSONObject(responseStr)

	var response QuestResponse
	if err := json.Unmarshal([]byte(responseStr), &response); err != nil {
		return nil, fmt.Errorf("failed to parse quest response: %w", err)
	}

	logger.Info(ctx, "quest generation completed",
		slog.Int("evaluations", len(response.QuestEvaluations)),
		slog.Int("newQuests", len(response.NewQuests)),
	)
	return &response, nil
}

func knownQuestIDSet(request QuestRequest) map[int64]struct{} {
	ids := make(map[int64]struct{}, len(request.Quests))
	for _, q := range request.Quests {
		if q.UserAiQuestID <= 0 {
			continue
		}
		ids[q.UserAiQuestID] = struct{}{}
	}
	return ids
}

// SaveResults는 quest 평가 결과와 신규 퀘스트를 DB에 저장하고, 완료된 퀘스트 ID와 신규 퀘스트 ID를 반환합니다.
func SaveResults(ctx context.Context, jobID int64, projectID int64, userID int64, request QuestRequest, resp *QuestResponse) (completedIDs []int64, newQuestIDs []int64, milestoneLinks []db.QuestMilestoneLinkInput) {
	completedIDs = make([]int64, 0)
	newQuestIDs = make([]int64, 0)
	milestoneLinks = make([]db.QuestMilestoneLinkInput, 0)
	knownQuestIDs := knownQuestIDSet(request)
	savedEvaluationCount := 0
	skippedEvaluationCount := 0

	for _, eval := range resp.QuestEvaluations {
		if _, ok := knownQuestIDs[eval.UserAiQuestID]; !ok {
			skippedEvaluationCount++
			logger.Warn(ctx, "skipping quest evaluation for unknown quest id",
				slog.Int64("questId", eval.UserAiQuestID),
				slog.Int64("jobId", jobID),
			)
			continue
		}

		if err := db.InsertQuestEvaluation(eval.UserAiQuestID, jobID, eval.EvaluationResult, eval.ConfidenceScore, eval.Reason, eval.ProgressNote); err != nil {
			logger.Error(ctx, "failed to insert quest evaluation", err, slog.Int64("questId", eval.UserAiQuestID))
			continue
		}
		savedEvaluationCount++

		if err := db.UpdateQuestLastEvaluatedAt(eval.UserAiQuestID); err != nil {
			logger.Warn(ctx, "failed to update quest last_evaluated_at",
				slog.Int64("questId", eval.UserAiQuestID),
				slog.String("reason", err.Error()),
			)
		}
		if eval.EvaluationResult == "COMPLETED" {
			if err := db.CompleteQuest(eval.UserAiQuestID); err != nil {
				logger.Warn(ctx, "failed to complete quest",
					slog.Int64("questId", eval.UserAiQuestID),
					slog.String("reason", err.Error()),
				)
			} else {
				completedIDs = append(completedIDs, eval.UserAiQuestID)
			}
		}
	}

	for _, nq := range resp.NewQuests {
		id, err := db.InsertQuest(projectID, userID, nq.Title, nq.Description, nq.Hint, nq.AIGenerationReason, nq.CompletionGuide, nq.RewardExp, nq.ExpiredAt)
		if err != nil {
			logger.Error(ctx, "failed to insert new quest", err, slog.String("title", nq.Title))
		} else {
			newQuestIDs = append(newQuestIDs, id)
			if nq.RelatedMilestoneKey != "" {
				milestoneLinks = append(milestoneLinks, db.QuestMilestoneLinkInput{
					UserAiQuestID: id,
					MilestoneKey:  nq.RelatedMilestoneKey,
				})
			}
		}
	}

	logger.Info(ctx, "quest results saved",
		slog.Int("savedEvaluations", savedEvaluationCount),
		slog.Int("skippedEvaluations", skippedEvaluationCount),
		slog.Int("newQuests", len(newQuestIDs)),
		slog.Int("questMilestoneLinks", len(milestoneLinks)),
	)
	return
}
