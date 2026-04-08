package disk

import (
	"worker_GoVer/disk/strategy"
)

var Disk strategy.DiskStrategy

// 해당 파일이 존재하는지 확인한 후 있으면 true없으면 false반환
func IsLocked(filePath string) (bool, error) {
	return Disk.IsLocked(filePath)
}

// 해당 폴더가 존재하는지 확인한 후 있으면 true없으면 false반환
func IsExistDir(dirPath string) (bool, error) {
	return Disk.IsExistDir(dirPath)
}

// .lock파일을 원자적으로 생성 실패시 error반환, 성공시 .lock파일 path반환
// .lock파일에는 생성시각을 ISO 8601형식으로 저장할것(서울시각기준)
// 이미 .lock파일이 있을경우 시간을 업데이트함
func CreateLockFileAtomic(filePath string) (string, error) {
	return Disk.CreateLockFileAtomic(filePath)
}

// .touch파일을 원자적으로 생성 실패지 error반환, 성공시 .touch파일 path반환
// .touch파일에는 생성시각을 ISO 8601형식으로 저장할것(서울시각기준)
// 이미 touch파일이 있을경우 시간을 업데이트함
func CreateTouchFileAtomic(filePath string) (string, error) {
	return Disk.CreateTouchFileAtomic(filePath)
}

// 원자적으로 .lock파일 삭제
func RemoveLockAtomic(filePath string) error {
	return Disk.RemoveLockAtomic(filePath)
}

// config에서 WorkspaceBaseDir을 이용하여 가져온 후
// 현재 실행되는 컴퓨터의 disk사용량(디스크가 여러개일 경우 실행되는 곳 기반 : config보면 나옴)이 70%이상 이면
// WorkspaceBaseDir/{projectName}/.touch파일 안에 존재하는 시간을 기준으로 {projectName}폴더단위로 가장 오래된 것들을 지운다
// 또한 .lock이 함수 실행 시간기준으로 24시가 지나면 삭제 후보로 등극
func IfNeedDoCleanWorkspace() error {
	return Disk.IfNeedDoCleanWorkspace()
}
