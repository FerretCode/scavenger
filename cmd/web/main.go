package main

import (
	"context"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	run "cloud.google.com/go/run/apiv2"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"github.com/ferretcode/scavenger/internal/auth"
	"github.com/ferretcode/scavenger/internal/workflow"
	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"google.golang.org/api/option"
)

var templates *template.Template
var logger *slog.Logger

func parseTemplates() error {
	var err error

	files := []string{
		"./views/dashboard.html",
		"./views/workflows.html",
		"./views/login.html",
	}

	templates, err = template.ParseFiles(files...)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx := context.Background()

	err := godotenv.Load()
	if err != nil {
		logger.Error("error parsing .env", "err", err)
		return
	}

	err = parseTemplates()
	if err != nil {
		logger.Error("error parsing templates", "err", err)
		return
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("database url does not exist in the environment variables")
		return
	}

	if _, err = os.Stat("./credentials.json"); err != nil {
		logger.Error("error parsing credentials file", "err", err)
		return
	}

	secretManagerClient, err := secretmanager.NewClient(ctx, option.WithCredentialsFile("./credentials.json"))
	if err != nil {
		logger.Error("error using credentials file", "err", err)
		return
	}
	defer secretManagerClient.Close()
	_ = secretManagerClient

	runClient, err := run.NewServicesClient(ctx, option.WithCredentialsFile("./credentials.json"))
	if err != nil {
		logger.Error("error creating google run client", "err", err)
		return
	}

	// Connect to MongoDB
	db, err := mongo.Connect(options.Client().ApplyURI(dsn))
	if err != nil {
		logger.Error("error connecting to mongodb database", "err", err)
		return
	}
	defer func() {
		if err = db.Disconnect(context.Background()); err != nil {
			panic(err)
		}
	}()

	// Ping the database
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = db.Ping(ctx, readpref.Primary())

	// CODE STARTS HERE
	r := chi.NewRouter()

	type workflows struct {
		Name   string
		URL    string
		Cron   string
		Prompt string
	}

	type dashboardData struct {
		Workflows []workflows
	}

	mockWorkflows := dashboardData{
		Workflows: []workflows{
			{
				Name:   "Workflow 1",
				URL:    "http://example.com/workflow1",
				Cron:   "0 0 * * *",
				Prompt: "Run the first workflow every midnight",
			},
			{
				Name:   "Workflow 2",
				URL:    "http://example.com/workflow2",
				Cron:   "0 6 * * *",
				Prompt: "Run the second workflow every morning at 6 AM",
			},
			{
				Name:   "Workflow 3",
				URL:    "http://example.com/workflow3",
				Cron:   "0 12 * * *",
				Prompt: "Run the third workflow every day at noon",
			},
			{
				Name:   "Workflow 4",
				URL:    "http://example.com/workflow4",
				Cron:   "0 18 * * *",
				Prompt: "Run the fourth workflow every evening at 6 PM",
			},
		},
	}

	r.With(auth.RequireAuth).Get("/", func(w http.ResponseWriter, r *http.Request) {
		t := template.Must(template.ParseFiles("views/dashboard.html"))
		t.Execute(w, mockWorkflows)
	})

	r.With(auth.RequireAuth).Get("/workflows", func(w http.ResponseWriter, r *http.Request) {
		t := template.Must(template.ParseFiles("views/workflows.html"))
		t.Execute(w, nil)
	})

	r.Route("/workflow", func(r chi.Router) {
		r.Post("/create", func(w http.ResponseWriter, r *http.Request) {
			handleError(workflow.Create(w, r, db, runClient, &ctx), w, "workflow/create")
		})
	})

	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
			handleError(auth.RenderLogin(w, r, templates), w, "login/render")
		})

		r.Post("/login", func(w http.ResponseWriter, r *http.Request) {
			handleError(auth.Login(w, r), w, "login")
		})

		r.Get("/logout", func(w http.ResponseWriter, r *http.Request) {
			handleError(auth.Logout(w, r), w, "logout")
		})
	})

	log.Println("Running web server http://localhost:3000")
	err = http.ListenAndServe(":3000", r)
	if err != nil {
		logger.Error("error serving http server", "error", err)
		return
	}
}

func handleError(err error, w http.ResponseWriter, svc string) {
	if err != nil {
		http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
		logger.Error("error processing request", "svc", svc, "err", err)
	}
}

// taking the host req
// creatign a reverse proxy using httputil
func connectingHostToUser(hostString string) (*httputil.ReverseProxy, error) {
	url, err := url.Parse(hostString)
	if err != nil {
		return nil, err
	}
	return httputil.NewSingleHostReverseProxy(url), nil
}

func proxyRequestHandler(proxy *httputil.ReverseProxy) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	}
}
