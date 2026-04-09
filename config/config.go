package config

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func mustGetEnv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		log.Fatalf("required env missing: %s", key)
	}
	return value
}

func getEnv(key, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvAsInt(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Fatalf("invalid int env %s=%q", key, value)
	}
	return parsed
}

func getEnvAsBool(key string, defaultValue bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		log.Fatalf("invalid bool env %s=%q", key, value)
	}
	return parsed
}

func getExecutedPath() string {
	exePath, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return exePath
}

func getWorkspacePath() string {
	return filepath.Join(filepath.Dir(getExecutedPath()), "disk")
}

var cfg *Config

func Get() *Config {
	return cfg
}

// loadPrivateKey는 환경변수로 깨지기 쉬운 PEM 문자열을 정상 PEM 형식으로 복구합니다.
func loadPrivateKey() string {
	raw := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if raw == "" {
		log.Fatalf("required env missing: GITHUB_APP_PRIVATE_KEY")
	}

	// \\n → 실제 개행
	raw = strings.ReplaceAll(raw, `\n`, "\n")
	raw = strings.TrimSpace(raw)

	// 이미 멀티라인 PEM이면 그대로 사용
	if strings.Count(raw, "\n") > 1 {
		return raw
	}

	// 한 줄 PEM → BEGIN/END 사이 본문을 64자 단위로 줄바꿈해서 복원
	const header = "-----BEGIN RSA PRIVATE KEY-----"
	const footer = "-----END RSA PRIVATE KEY-----"

	body := raw
	body = strings.ReplaceAll(body, header, "")
	body = strings.ReplaceAll(body, footer, "")
	body = strings.ReplaceAll(body, " ", "")
	body = strings.TrimSpace(body)

	var sb strings.Builder
	sb.WriteString(header + "\n")
	for len(body) > 0 {
		chunk := body
		if len(chunk) > 64 {
			chunk = body[:64]
		}
		sb.WriteString(chunk + "\n")
		body = body[len(chunk):]
	}
	sb.WriteString(footer + "\n")

	return sb.String()
}

func LoadConfig() {
	cfg = &Config{
		WorkspaceBaseDir:        getEnv("WORKSPACE_BASE_DIR", getWorkspacePath()),
		DBHost:                  mustGetEnv("DB_HOST"),
		DBPort:                  getEnvAsInt("DB_PORT", 3306),
		DBUser:                  mustGetEnv("DB_USER"),
		DBPass:                  mustGetEnv("DB_PASS"),
		DBName:                  mustGetEnv("DB_NAME"),
		OpenAIKey:               mustGetEnv("OPENAI_API_KEY"),
		DefaultModel:            getEnv("DEFAULT_MODEL", "gpt-4o-mini"),
		GitHubAppID:             mustGetEnv("GITHUB_APP_ID"),
		GitHubAppPrivateKey:     loadPrivateKey(),
		GitHubClientID:          mustGetEnv("GITHUB_CLIENT_ID"),
		GitHubClientSecret:      mustGetEnv("GITHUB_CLIENT_SECRET"),
		GitHubAppStateSecret:    mustGetEnv("GITHUB_APP_STATE_SECRET"),
		Debug:                   getEnvAsBool("DEBUG", false),
		AWSRegion:               mustGetEnv("AWS_REGION"),
		AWSAccessKeyID:          mustGetEnv("AWS_ACCESS_KEY_ID"),
		AWSSecretAccessKey:      mustGetEnv("AWS_SECRET_ACCESS_KEY"),
		AWSAnalysisQueueURL:     mustGetEnv("AWS_ANALYSIS_QUEUE_URL"),
		AWSNotificationQueueURL: mustGetEnv("AWS_NOTIFICATION_QUEUE_URL"),
		AWSS3Bucket:             mustGetEnv("AWS_S3_BUCKET"),
		SQSConsumerConcurrency:  getEnvAsInt("SQS_CONSUMER_CONCURRENCY", 5),
		SQSMaxNumberOfMessages:  getEnvAsInt("SQS_MAX_NUMBER_OF_MESSAGES", 10),
		DBMaxOpenConns:          getEnvAsInt("DB_MAX_OPEN_CONNS", 10),
		DBMaxIdleConns:          getEnvAsInt("DB_MAX_IDLE_CONNS", 5),
		DBConnMaxLifetimeMin:    getEnvAsInt("DB_CONN_MAX_LIFETIME_MIN", 5),
		LogDirectory:            getEnv("LOG_DIRECTORY", "./.logs"),
	}
}
