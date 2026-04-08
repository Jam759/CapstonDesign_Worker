package config

type Config struct {
	DBHost                  string
	DBPort                  int
	DBUser                  string
	DBPass                  string
	DBName                  string
	OpenAIKey               string
	DefaultModel            string
	WorkspaceBaseDir        string
	GitHubAppID             string
	GitHubAppPrivateKey     string
	GitHubClientID          string
	GitHubClientSecret      string
	GitHubAppStateSecret    string
	Debug                   bool
	AWSRegion               string
	AWSAccessKeyID          string
	AWSSecretAccessKey      string
	AWSAnalysisQueueURL     string
	AWSNotificationQueueURL string
	AWSS3Bucket             string
	SQSConsumerConcurrency  int
	SQSMaxNumberOfMessages  int
	DBMaxOpenConns          int
	DBMaxIdleConns          int
	DBConnMaxLifetimeMin    int
}
