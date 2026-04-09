package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxLogFileSizeBytes  = 20 * 1024 * 1024
	maxLogTotalSizeBytes = 1024 * 1024 * 1024
	maxLogAge            = 14 * 24 * time.Hour
)

type rotatingFileWriter struct {
	dir        string
	archiveDir string
	activePath string

	mu   sync.Mutex
	file *os.File
	size int64
}

func newRotatingFileWriter(dir string) (*rotatingFileWriter, error) {
	writer := &rotatingFileWriter{
		dir:        dir,
		archiveDir: filepath.Join(dir, "archive"),
		activePath: filepath.Join(dir, "worker.log"),
	}

	if err := os.MkdirAll(writer.dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}
	if err := os.MkdirAll(writer.archiveDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log archive directory: %w", err)
	}
	if err := writer.openActiveFile(); err != nil {
		return nil, err
	}
	if err := writer.cleanupArchives(); err != nil {
		return nil, err
	}

	return writer, nil
}

func (w *rotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.openActiveFile(); err != nil {
			return 0, err
		}
	}

	if w.size+int64(len(p)) > maxLogFileSizeBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingFileWriter) openActiveFile() error {
	file, err := os.OpenFile(w.activePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open active log file: %w", err)
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to stat active log file: %w", err)
	}

	w.file = file
	w.size = info.Size()
	return nil
}

func (w *rotatingFileWriter) rotate() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("failed to close active log file: %w", err)
		}
		w.file = nil
	}

	info, err := os.Stat(w.activePath)
	if err == nil && info.Size() > 0 {
		archivePath, err := w.nextArchivePath()
		if err != nil {
			return err
		}
		if err := os.Rename(w.activePath, archivePath); err != nil {
			return fmt.Errorf("failed to archive log file: %w", err)
		}
	}

	if err := w.openActiveFile(); err != nil {
		return err
	}
	return w.cleanupArchives()
}

func (w *rotatingFileWriter) nextArchivePath() (string, error) {
	dateKey := time.Now().In(time.FixedZone("KST", 9*60*60)).Format("2006-01-02")
	pattern := fmt.Sprintf("worker.%s.*.log", dateKey)

	entries, err := filepath.Glob(filepath.Join(w.archiveDir, pattern))
	if err != nil {
		return "", fmt.Errorf("failed to list archived log files: %w", err)
	}

	maxIndex := -1
	for _, entry := range entries {
		base := filepath.Base(entry)
		parts := strings.Split(base, ".")
		if len(parts) != 5 {
			continue
		}
		var idx int
		if _, err := fmt.Sscanf(parts[3], "%d", &idx); err == nil && idx > maxIndex {
			maxIndex = idx
		}
	}

	return filepath.Join(w.archiveDir, fmt.Sprintf("worker.%s.%d.log", dateKey, maxIndex+1)), nil
}

func (w *rotatingFileWriter) cleanupArchives() error {
	entries, err := os.ReadDir(w.archiveDir)
	if err != nil {
		return fmt.Errorf("failed to read log archive directory: %w", err)
	}

	type archiveFile struct {
		path    string
		modTime time.Time
		size    int64
	}

	now := time.Now()
	files := make([]archiveFile, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to read archived log info: %w", err)
		}
		path := filepath.Join(w.archiveDir, entry.Name())
		if now.Sub(info.ModTime()) > maxLogAge {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("failed to remove expired archived log: %w", err)
			}
			continue
		}
		files = append(files, archiveFile{
			path:    path,
			modTime: info.ModTime(),
			size:    info.Size(),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	var totalSize int64
	for _, file := range files {
		totalSize += file.size
	}

	for _, file := range files {
		if totalSize <= maxLogTotalSizeBytes {
			break
		}
		if err := os.Remove(file.path); err != nil {
			return fmt.Errorf("failed to trim archived log: %w", err)
		}
		totalSize -= file.size
	}

	return nil
}
