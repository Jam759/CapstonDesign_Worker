package git

// FileDiffStat은 변경된 파일 정보를 나타냅니다.
type FileDiffStat struct {
	Path         string
	PreviousPath string // rename 시 이전 경로
	Status       string // "added", "modified", "deleted", "renamed"
}
