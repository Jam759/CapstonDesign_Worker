//go:build windows

package disk

import (
	"worker_GoVer/config"
	"worker_GoVer/disk/strategy"
)

func Init() error {
	cfg := config.Get()
	Disk = &strategy.WindowsDiskStrategy{WorkspaceBaseDir: cfg.WorkspaceBaseDir}
	return nil
}
