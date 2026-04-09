package disk

import (
	"context"
	"worker_GoVer/disk/strategy"
)

var Disk strategy.DiskStrategy

// 해당 파일이 존재하는지 확인한 후 있으면 true없으면 false반환
func IsLocked(ctx context.Context, filePath string) (bool, error) {
	_ = ctx
	return Disk.IsLocked(filePath)
}

// 해당 폴더가 존재하는지 확인한 후 있으면 true없으면 false반환
func IsExistDir(ctx context.Context, dirPath string) (bool, error) {
	_ = ctx
	return Disk.IsExistDir(dirPath)
}

// .lock파일을 원자적으로 생성 실패시 error반환, 성공시 .lock파일 path반환
func CreateLockFileAtomic(ctx context.Context, filePath string) (string, error) {
	_ = ctx
	return Disk.CreateLockFileAtomic(filePath)
}

// .touch파일을 원자적으로 생성 실패지 error반환, 성공시 .touch파일 path반환
func CreateTouchFileAtomic(ctx context.Context, filePath string) (string, error) {
	_ = ctx
	return Disk.CreateTouchFileAtomic(filePath)
}

// 원자적으로 .lock파일 삭제
func RemoveLockAtomic(ctx context.Context, filePath string) error {
	_ = ctx
	return Disk.RemoveLockAtomic(filePath)
}

// config에서 WorkspaceBaseDir을 이용하여 가져온 후 디스크 사용량이 높으면 오래된 프로젝트 디렉토리를 정리합니다.
func IfNeedDoCleanWorkspace(ctx context.Context) error {
	_ = ctx
	return Disk.IfNeedDoCleanWorkspace()
}
