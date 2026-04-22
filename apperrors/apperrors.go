package apperrors

import "fmt"

type ErrorCode string

const (
	ErrUnmarshalMessage    ErrorCode = "UNMARSHAL_MESSAGE"
	ErrInvalidJobData      ErrorCode = "INVALID_JOB_DATA"
	ErrNoProjectKB         ErrorCode = "NO_PROJECT_KB"
	ErrWorkspaceLocked     ErrorCode = "WORKSPACE_LOCKED"
	ErrGitOperation        ErrorCode = "GIT_OPERATION_FAILED"
	ErrCodeGraphGeneration ErrorCode = "CODE_GRAPH_GENERATION_FAILED"
	ErrAIAnalysis          ErrorCode = "AI_ANALYSIS_FAILED"
	ErrS3Operation         ErrorCode = "S3_OPERATION_FAILED"
	ErrDBOperation         ErrorCode = "DB_OPERATION_FAILED"
)

// AnalysisError는 분석 작업 중 발생하는 커스텀 에러입니다.
type AnalysisError struct {
	Code       ErrorCode
	Message    string
	HTTPStatus int
	Retryable  bool
	Cause      error
}

func (e *AnalysisError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AnalysisError) Unwrap() error { return e.Cause }

// New는 AnalysisError를 생성합니다.
func New(code ErrorCode, httpStatus int, retryable bool, cause error) *AnalysisError {
	return &AnalysisError{
		Code:       code,
		Message:    string(code),
		HTTPStatus: httpStatus,
		Retryable:  retryable,
		Cause:      cause,
	}
}

// Newf는 메시지 포맷과 함께 AnalysisError를 생성합니다.
func Newf(code ErrorCode, httpStatus int, retryable bool, cause error, format string, args ...any) *AnalysisError {
	return &AnalysisError{
		Code:       code,
		Message:    fmt.Sprintf(format, args...),
		HTTPStatus: httpStatus,
		Retryable:  retryable,
		Cause:      cause,
	}
}
