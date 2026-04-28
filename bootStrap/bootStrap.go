package bootStrap

import (
	"context"
	stdlog "log"
	"log/slog"
	"os/signal"
	"syscall"
	"time"
	"worker_GoVer/config"
	"worker_GoVer/db"
	"worker_GoVer/disk"
	"worker_GoVer/logger"
	"worker_GoVer/metrics"
	"worker_GoVer/s3"
	"worker_GoVer/sqs"
)

var log = logger.WithComponent("worker")

func AppStart() {
	stdlog.Printf("boot start....")
	//1. 설정
	config.LoadConfig()
	cfg := config.Get()
	stdlog.Printf("config loaded")

	//2. 로거 초기화 (설정 로드 이후 가장 먼저)
	if err := logger.Init(logger.Config{
		Directory:  cfg.LogDirectory,
		Debug:      cfg.Debug,
		Service:    "capstone-worker",
		ServerType: "go",
	}); err != nil {
		stdlog.Fatalf("failed to init logger: %v", err)
	}
	log.WorkerEvent(context.Background(), logger.EventWorkerStarted, "logger initialized")

	metricsServer, err := metrics.StartServer(cfg.MetricsListenAddr)
	if err != nil {
		log.Warn(context.Background(), "failed to start metrics server", err,
			slog.String("listenAddr", cfg.MetricsListenAddr),
		)
	} else {
		log.WorkerEvent(context.Background(), logger.EventWorkerStarted, "metrics server started",
			slog.String("listenAddr", cfg.MetricsListenAddr),
		)
	}

	//3. 디스크 설정(OS에 따라 각 OS에 맞는 구현체가 주입됨)
	if err := disk.Init(); err != nil {
		log.Error(context.Background(), "disk init failed", err)
		stdlog.Fatal(err)
	}
	log.WorkerEvent(context.Background(), logger.EventWorkerStarted, "disk initialized")

	//4. DB 연결
	if err := db.Init(); err != nil {
		log.Error(context.Background(), "DB init failed", err)
		stdlog.Fatalf("failed to init DB: %v", err)
	}
	log.WorkerEvent(context.Background(), logger.EventWorkerStarted, "DB connected")

	//5. S3 클라이언트 초기화
	if err := s3.Init(); err != nil {
		log.Error(context.Background(), "S3 init failed", err)
		stdlog.Fatalf("failed to init S3: %v", err)
	}
	log.WorkerEvent(context.Background(), logger.EventWorkerStarted, "S3 initialized")

	//6. SQS 컨슈머 시작
	consumer, err := sqs.NewConsumer()
	if err != nil {
		log.Error(context.Background(), "SQS consumer creation failed", err)
		stdlog.Fatalf("failed to create SQS consumer: %v", err)
	}
	log.WorkerEvent(context.Background(), logger.EventWorkerStarted, "SQS consumer created")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Warn(context.Background(), "failed to shutdown metrics server", err)
		}
	}()

	go consumer.StartAnalysisListener(ctx)

	log.WorkerEvent(context.Background(), logger.EventWorkerStarted, "worker ready")
	<-ctx.Done()
	log.WorkerEvent(context.Background(), logger.EventWorkerStopping, "shutdown signal received, waiting for in-flight jobs")
}
