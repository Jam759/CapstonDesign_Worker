//go:build linux || darwin

package strategy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

type LinuxDiskStrategy struct {
	WorkspaceBaseDir string
}

func (l *LinuxDiskStrategy) IsLocked(projectPath string) (bool, error) {
	lockPath := filepath.Join(projectPath, ".lock")
	_, err := os.Stat(lockPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check lock file: %w", err)
}

func (l *LinuxDiskStrategy) IsExistDir(dirPath string) (bool, error) {
	info, err := os.Stat(dirPath)
	if err == nil {
		return info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check directory: %w", err)
}

func (l *LinuxDiskStrategy) CreateLockFileAtomic(dirPath string) (string, error) {
	lockPath := filepath.Join(dirPath, ".lock")
	return l.writeFileAtomic(lockPath)
}

func (l *LinuxDiskStrategy) CreateTouchFileAtomic(dirPath string) (string, error) {
	touchPath := filepath.Join(dirPath, ".touch")
	return l.writeFileAtomic(touchPath)
}

func (l *LinuxDiskStrategy) writeFileAtomic(targetPath string) (string, error) {
	seoul, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return "", fmt.Errorf("failed to load Seoul timezone: %w", err)
	}
	now := time.Now().In(seoul).Format(time.RFC3339)

	dir := filepath.Dir(targetPath)
	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(now); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write to temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to rename temp file atomically: %w", err)
	}

	return targetPath, nil
}

func (l *LinuxDiskStrategy) RemoveLockAtomic(dirPath string) error {
	lockPath := filepath.Join(dirPath, ".lock")
	if err := os.Remove(lockPath); err != nil {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}
	return nil
}

func (l *LinuxDiskStrategy) IfNeedDoCleanWorkspace() error {
	usage, err := l.getDiskUsagePercent(l.WorkspaceBaseDir)
	if err != nil {
		return fmt.Errorf("failed to get disk usage: %w", err)
	}
	if usage < 70 {
		return nil
	}

	entries, err := os.ReadDir(l.WorkspaceBaseDir)
	if err != nil {
		return fmt.Errorf("failed to read workspace dir: %w", err)
	}

	type projectInfo struct {
		name      string
		touchTime time.Time
	}

	now := time.Now()
	var candidates []projectInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projPath := filepath.Join(l.WorkspaceBaseDir, entry.Name())

		// .lock 파일 확인: 24시간 이내면 삭제 후보에서 제외
		lockPath := filepath.Join(projPath, ".lock")
		lockData, err := os.ReadFile(lockPath)
		if err == nil {
			lockTime, err := time.Parse(time.RFC3339, string(lockData))
			if err == nil && now.Sub(lockTime) <= 24*time.Hour {
				continue
			}
			// 24시간 지난 .lock → 프로젝트 폴더를 삭제 후보로 등록
		}

		// .touch 파일 시간 읽기
		var touchTime time.Time
		touchPath := filepath.Join(projPath, ".touch")
		touchData, err := os.ReadFile(touchPath)
		if err == nil {
			t, err := time.Parse(time.RFC3339, string(touchData))
			if err == nil {
				touchTime = t
			}
		}

		candidates = append(candidates, projectInfo{
			name:      entry.Name(),
			touchTime: touchTime,
		})
	}

	// 오래된 순으로 정렬
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].touchTime.Before(candidates[j].touchTime)
	})

	// 디스크 사용량이 30% 이하가 될 때까지 삭제
	for _, proj := range candidates {
		usage, err = l.getDiskUsagePercent(l.WorkspaceBaseDir)
		if err != nil {
			return fmt.Errorf("failed to get disk usage: %w", err)
		}
		if usage <= 30 {
			break
		}

		projPath := filepath.Join(l.WorkspaceBaseDir, proj.name)
		if err := os.RemoveAll(projPath); err != nil {
			return fmt.Errorf("failed to remove project %s: %w", proj.name, err)
		}
	}

	return nil
}

func (l *LinuxDiskStrategy) getDiskUsagePercent(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("failed to get filesystem stats: %w", err)
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free
	return float64(used) / float64(total) * 100, nil
}
