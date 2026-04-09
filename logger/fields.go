package logger

// Category는 메인서버 로그 포맷의 category 필드 값입니다.
const (
	CategorySQS      = "SQS"
	CategoryAnalysis = "ANALYSIS"
	CategoryJobStep  = "JOB_STEP"
	CategoryWorker   = "WORKER"
)

// EventType은 메인서버 로그 포맷의 eventType 필드 값입니다.
const (
	// SQS category
	EventSQSReceived  = "SQS_RECEIVED"
	EventSQSProcessed = "SQS_PROCESSED"
	EventSQSFailed    = "SQS_FAILED"

	// ANALYSIS category
	EventAnalysisStarted   = "ANALYSIS_STARTED"
	EventAnalysisCompleted = "ANALYSIS_COMPLETED"
	EventAnalysisFailed    = "ANALYSIS_FAILED"

	// JOB_STEP category
	EventStepStarted   = "STEP_STARTED"
	EventStepCompleted = "STEP_COMPLETED"
	EventStepFailed    = "STEP_FAILED"

	// WORKER category (부트/셧다운 등 인프라 이벤트)
	EventWorkerStarted  = "WORKER_STARTED"
	EventWorkerStopping = "WORKER_STOPPING"
	EventWorkerStopped  = "WORKER_STOPPED"
)
