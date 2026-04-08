package bootStrap

import (
	"context"
	"log"
	"worker_GoVer/config"
	"worker_GoVer/db"
	"worker_GoVer/disk"
	"worker_GoVer/s3"
	"worker_GoVer/sqs"
)

func AppStart() {
	log.Println("[Boot] starting...")

	//1. 설정
	config.LoadConfig()
	log.Println("[Boot] config loaded")

	//2. 디스크 설정(OS에 따라 각 OS에 맞는 구현체가 주입됨)
	if err := disk.Init(); err != nil {
		log.Fatal(err)
	}
	log.Println("[Boot] disk initialized")

	//3. DB 연결
	if err := db.Init(); err != nil {
		log.Fatalf("failed to init DB: %v", err)
	}
	log.Println("[Boot] DB connected")

	//4. S3 클라이언트 초기화
	if err := s3.Init(); err != nil {
		log.Fatalf("failed to init S3: %v", err)
	}
	log.Println("[Boot] S3 initialized")

	//5. SQS 컨슈머 시작
	consumer, err := sqs.NewConsumer()
	if err != nil {
		log.Fatalf("failed to create SQS consumer: %v", err)
	}
	log.Println("[Boot] SQS consumer created")

	ctx := context.Background()
	go consumer.StartAnalysisListener(ctx)

	log.Println("[Boot] ready")
	// 메인 고루틴 블로킹
	select {}
}
