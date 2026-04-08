package strategy

type DiskStrategy interface {
	IsLocked(projectPath string) (bool, error)
	IsExistDir(dirPath string) (bool, error)
	CreateLockFileAtomic(dirPath string) (string, error)
	CreateTouchFileAtomic(dirPath string) (string, error)
	RemoveLockAtomic(dirPath string) error
	IfNeedDoCleanWorkspace() error
}
