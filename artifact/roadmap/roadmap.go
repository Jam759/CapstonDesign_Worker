package roadmap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"worker_GoVer/ai"
	"worker_GoVer/db"
	"worker_GoVer/logger"
)

var log = logger.WithComponent("roadmap")

func Generate(ctx context.Context, input GenerateInput, projectContextPath string) (*Plan, error) {
	metadata, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal roadmap metadata: %w", err)
	}

	p := ai.RoadMapPrompt(string(metadata))
	log.Trace(ctx, "roadmap generation start",
		slog.Int64("projectId", input.ProjectID),
		slog.String("repo", input.RepositoryFullName),
	)
	result := <-ai.GenerateMessageWithFiles(ctx, p.User, p.System, []string{projectContextPath})
	if result.Err != nil {
		return nil, fmt.Errorf("AI roadmap generation failed: %w", result.Err)
	}

	responseStr, ok := result.Data.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected AI response type")
	}
	responseStr = cleanTrailingCommas(extractJSONObject(responseStr))

	var plan Plan
	if err := json.Unmarshal([]byte(responseStr), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse roadmap response: %w", err)
	}
	if err := plan.Normalize(); err != nil {
		return nil, err
	}

	log.Trace(ctx, "roadmap generation completed",
		slog.Int("phaseCount", len(plan.Phases)),
		slog.Int("milestoneCount", len(plan.Milestones)),
	)
	return &plan, nil
}

func Persist(ctx context.Context, projectID int64, plan *Plan, questLinks []db.QuestMilestoneLinkInput) (*db.RoadMapSaveResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("roadmap plan is nil")
	}
	if err := plan.Normalize(); err != nil {
		return nil, err
	}

	result, err := db.ReplaceProjectRoadMap(projectID, plan.ToDBInputs(), questLinks)
	if err != nil {
		return nil, err
	}

	log.Trace(ctx, "roadmap saved",
		slog.Int64("projectId", projectID),
		slog.Int("phaseCount", len(result.PhaseIDs)),
		slog.Int("milestoneCount", len(result.MilestoneIDs)),
		slog.Int("linkedQuestCount", len(result.LinkedQuestIDs)),
		slog.Int("skippedQuestLinkCount", len(result.SkippedQuestLinks)),
	)
	if len(result.SkippedQuestLinks) > 0 {
		log.Warn(ctx, "some quest roadmap links were skipped", nil,
			slog.Int64("projectId", projectID),
			slog.Int("skippedQuestLinkCount", len(result.SkippedQuestLinks)),
		)
	}
	return result, nil
}

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i != -1 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

var trailingCommaRe = regexp.MustCompile(`,\s*([}\]])`)

func cleanTrailingCommas(s string) string {
	return trailingCommaRe.ReplaceAllString(s, "$1")
}
