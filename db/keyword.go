package db

import (
	"fmt"
	"time"
)

type ProjectSearchKeyword struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement"`
	ProjectID     int64     `gorm:"column:project_id;not null"`
	JobID         int64     `gorm:"column:job_id;not null"`
	PlatformCmdID string    `gorm:"column:platform_cmd_id;not null"`
	Keyword       string    `gorm:"column:keyword;not null"`
	DisplayOrder  int       `gorm:"column:display_order;not null"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (ProjectSearchKeyword) TableName() string { return "project_search_keywords" }

type KeywordInsertInput struct {
	ProjectID    int64
	JobID        int64
	Platform     string
	Keyword      string
	DisplayOrder int
}

func InsertKeywords(inputs []KeywordInsertInput) error {
	if len(inputs) == 0 {
		return nil
	}
	records := make([]ProjectSearchKeyword, 0, len(inputs))
	for _, inp := range inputs {
		records = append(records, ProjectSearchKeyword{
			ProjectID:     inp.ProjectID,
			JobID:         inp.JobID,
			PlatformCmdID: inp.Platform,
			Keyword:       inp.Keyword,
			DisplayOrder:  inp.DisplayOrder,
		})
	}
	if err := conn.Create(&records).Error; err != nil {
		return fmt.Errorf("failed to insert keywords: %w", err)
	}
	return nil
}
