package types

type ScavengerConfig struct {
	DatabaseUrl        string `env:"DATABASE_URL"`
	DatabaseName       string `env:"DATABASE_NAME"`
	GcpProjectId       string `env:"GCP_PROJECT_ID"`
	GcpCredentialsJson string `env:"GCP_CREDENTIALS_JSON"`
	GcpLocation        string `env:"GCP_LOCATION"`
	GeminiApiKey       string `env:"GEMINI_API_KEY"`
	SessionsCookieName string `env:"SESSIONS_COOKIE_NAME"`
	AdminUsername      string `env:"ADMIN_USERNAME"`
	AdminPassword      string `env:"ADMIN_PASSWORD"`
	Provider           string `env:"PROVIDER"`
	WorkerImage        string `env:"WORKER_IMAGE"`
}
