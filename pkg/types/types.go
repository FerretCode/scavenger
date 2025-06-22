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
	HeadlessApiKey     string `env:"HEADLESS_API_KEY"`
	Mode               string `env:"MODE"`
}

type WorkflowsConfig struct {
	Name    string                         `json:"name"`
	Prompt  string                         `json:"prompt"`
	Cron    string                         `json:"cron"`
	Website string                         `json:"website"`
	Schema  map[string]WorkflowSchemaField `json:"schema"`
}

type WorkflowSchemaField struct {
	Name string `json:"title"`
	Type string `json:"type"`
	Desc string `json:"description"`
}

type DashboardCardData struct {
	DocScraped  int
	CliConnects int
}
