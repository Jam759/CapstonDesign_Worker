//go:build linux || darwin

package disk

import (
	"worker_GoVer/config"
	"worker_GoVer/disk/strategy"
)

func Init() error {
	cfg := config.Get()
	Disk = &strategy.LinuxDiskStrategy{WorkspaceBaseDir: cfg.WorkspaceBaseDir}
	return nil
}
