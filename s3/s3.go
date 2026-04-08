package s3

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"worker_GoVer/config"

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
func UploadProjectContext(installationID int64, repoID int64, localFilePath string) (string, error) {
	key := fmt.Sprintf("projectContext/%d/%d/%s", installationID, repoID, filepath.Base(localFilePath))
	return upload(key, localFilePath)
}

// UploadUserView는 userView JSON 파일을 S3에 업로드합니다.
// S3 key: userView/{installationId}/{repoId}/{fileName}
func UploadUserView(installationID int64, repoID int64, localFilePath string) (string, error) {
	key := fmt.Sprintf("userView/%d/%d/%s", installationID, repoID, filepath.Base(localFilePath))
	return upload(key, localFilePath)
}

func upload(key string, localFilePath string) (string, error) {
	cfg := config.Get()
	log.Printf("[S3] uploading key=%s", key)

	f, err := os.Open(localFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	if _, err = client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.AWSS3Bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String("application/json"),
	}); err != nil {
		return "", fmt.Errorf("failed to upload to S3: %w", err)
	}

	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.AWSS3Bucket, cfg.AWSRegion, key)
	log.Printf("[S3] uploaded: %s", url)
	return url, nil
}
