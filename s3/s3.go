package s3

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"worker_GoVer/config"
	"worker_GoVer/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var client *s3.Client

func Init() error {
	cfg := config.Get()

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.AWSRegion),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AWSAccessKeyID, cfg.AWSSecretAccessKey, ""),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	client = s3.NewFromConfig(awsCfg)
	return nil
}

// UploadProjectContext는 projectContext JSON 파일을 S3에 업로드합니다.
// S3 key: projectContext/{installationId}/{repoId}/{fileName}
func UploadProjectContext(ctx context.Context, installationID int64, repoID int64, localFilePath string) (string, error) {
	key := fmt.Sprintf("projectContext/%d/%d/%s", installationID, repoID, filepath.Base(localFilePath))
	return upload(ctx, key, localFilePath)
}

// UploadUserView는 userView JSON 파일을 S3에 업로드합니다.
// S3 key: userView/{installationId}/{repoId}/{fileName}
func UploadUserView(ctx context.Context, installationID int64, repoID int64, localFilePath string) (string, error) {
	key := fmt.Sprintf("userView/%d/%d/%s", installationID, repoID, filepath.Base(localFilePath))
	return upload(ctx, key, localFilePath)
}

// DownloadProjectKB는 S3에 저장된 PROJECT_KB 파일을 destDir에 다운로드합니다.
func DownloadProjectKB(ctx context.Context, bucket string, storedURL string, destDir string) (string, error) {
	const sep = ".amazonaws.com/"
	idx := strings.Index(storedURL, sep)
	if idx < 0 {
		return "", fmt.Errorf("cannot parse S3 key from URL: %s", storedURL)
	}
	key := storedURL[idx+len(sep):]
	fileName := filepath.Base(key)

	logger.Info(ctx, "S3 download start", slog.String("key", key), slog.String("bucket", bucket))

	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get object from S3: %w", err)
	}
	defer resp.Body.Close()

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create dest dir: %w", err)
	}

	destPath := filepath.Join(destDir, fileName)
	f, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to create dest file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("failed to write downloaded file: %w", err)
	}

	logger.Info(ctx, "S3 download done", slog.String("destPath", destPath))
	return destPath, nil
}

func upload(ctx context.Context, key string, localFilePath string) (string, error) {
	cfg := config.Get()
	logger.Info(ctx, "S3 upload start", slog.String("key", key))

	f, err := os.Open(localFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	if _, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(cfg.AWSS3Bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String("application/json"),
	}); err != nil {
		return "", fmt.Errorf("failed to upload to S3: %w", err)
	}

	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.AWSS3Bucket, cfg.AWSRegion, key)
	logger.Info(ctx, "S3 upload done", slog.String("url", url))
	return url, nil
}
