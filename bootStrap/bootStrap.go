package bootStrap

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"worker_GoVer/config"
	"worker_GoVer/db"
	"worker_GoVer/disk"
	"worker_GoVer/logger"
	"worker_GoVer/s3"
	"worker_GoVer/sqs"
)

func AppStart() {


	logger.WorkerEvent(context.Background(), logger.EventWorkerStarted, "boot start....")
	//1. 설정
	config.LoadConfig()
	cfg := config.Get()
	logger.WorkerEvent(context.Background(), logger.EventWorkerStarted, "config loaded")

	//2. 로거 초기화 (설정 로드 이후 가장 먼저)
	if err := logger.Init(logger.Config{
		Directory:  cfg.LogDirectory,
		Debug:      cfg.Debug,
		Service:    "capstone-worker",
		ServerType: "go",
	}); err != nil {
		log.Fatalf("failed to init logger: %v", err)
	}
	logger.WorkerEvent(context.Background(), logger.EventWorkerStarted, "logger initialized")

	//3. 디스크 설정(OS에 따라 각 OS에 맞는 구현체가 주입됨)
	if err := disk.Init(); err != nil {
		logger.Error(context.Background(), "disk init failed", err)
		log.Fatal(err)
	}
	logger.WorkerEvent(context.Background(), logger.EventWorkerStarted, "disk initialized")

	//4. DB 연결
	if err := db.Init(); err != nil {
		logger.Error(context.Background(), "DB init failed", err)
		log.Fatalf("failed to init DB: %v", err)
	}
	logger.WorkerEvent(context.Background(), logger.EventWorkerStarted, "DB connected")

	//5. S3 클라이언트 초기화
	if err := s3.Init(); err != nil {
		logger.Error(context.Background(), "S3 init failed", err)
		log.Fatalf("failed to init S3: %v", err)
	}
	logger.WorkerEvent(context.Background(), logger.EventWorkerStarted, "S3 initialized")

	//6. SQS 컨슈머 시작
	consumer, err := sqs.NewConsumer()
	if err != nil {
		logger.Error(context.Background(), "SQS consumer creation failed", err)
		log.Fatalf("failed to create SQS consumer: %v", err)
	}
	logger.WorkerEvent(context.Background(), logger.EventWorkerStarted, "SQS consumer created")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go consumer.StartAnalysisListener(ctx)

	logger.WorkerEvent(context.Background(), logger.EventWorkerStarted, "worker ready")
	<-ctx.Done()
	logger.WorkerEvent(context.Background(), logger.EventWorkerStopping, "shutdown signal received, waiting for in-flight jobs")
}
