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
	"github.com/ferretcode/scavenger/internal/dashboard"
	"github.com/ferretcode/scavenger/internal/workflow"
	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
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
		"./views/api.html",
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
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = db.Ping(pingCtx, readpref.Primary())

	// CODE STARTS HERE
	r := chi.NewRouter()

	mockWorkflows := dashboard.DashboardData{
		Workflows: []workflow.Workflow{
			{
				Name:       "Workflow 1",
				ServiceUri: "http://example.com/workflow1",
				Cron:       "0 0 * * *",
				Prompt:     "Run the first workflow every midnight",
				Schema: workflow.Schema{
					Title: "Workflow 1 Schema",
					Type:  "object",
					Properties: map[string]workflow.Field{
						"field1": {
							Name: "Field 1",
							Type: "string",
							Desc: "The first field for Workflow 1",
						},
						"field2": {
							Name: "Field 2",
							Type: "integer",
							Desc: "The second field for Workflow 1",
						},
					},
					Required: []string{"field1"},
				},
			},
			{
				Name:       "Workflow 2",
				ServiceUri: "http://example.com/workflow2",
				Cron:       "0 6 * * *",
				Prompt:     "Run the second workflow every morning at 6 AM",
				Schema: workflow.Schema{
					Title: "Workflow 2 Schema",
					Type:  "object",
					Properties: map[string]workflow.Field{
						"field1": {
							Name: "Field 1",
							Type: "string",
							Desc: "The first field for Workflow 2",
						},
						"field2": {
							Name: "Field 2",
							Type: "boolean",
							Desc: "The second field for Workflow 2",
						},
					},
					Required: []string{"field1", "field2"},
				},
			},
			{
				Name:       "Workflow 3",
				ServiceUri: "http://example.com/workflow3",
				Cron:       "0 12 * * *",
				Prompt:     "Run the third workflow every day at noon",
				Schema: workflow.Schema{
					Title: "Workflow 3 Schema",
					Type:  "object",
					Properties: map[string]workflow.Field{
						"field1": {
							Name: "Field 1",
							Type: "string",
							Desc: "The first field for Workflow 3",
						},
						"field2": {
							Name: "Field 2",
							Type: "float",
							Desc: "The second field for Workflow 3",
						},
					},
					Required: []string{"field1"},
				},
			},
			{
				Name:       "Workflow 4",
				ServiceUri: "http://example.com/workflow4",
				Cron:       "0 18 * * *",
				Prompt:     "Run the fourth workflow every evening at 6 PM",
				Schema: workflow.Schema{
					Title: "Workflow 4 Schema",
					Type:  "object",
					Properties: map[string]workflow.Field{
						"field1": {
							Name: "Field 1",
							Type: "string",
							Desc: "The first field for Workflow 4",
						},
						"field2": {
							Name: "Field 2",
							Type: "integer",
							Desc: "The second field for Workflow 4",
						},
						"field3": {
							Name: "Field 3",
							Type: "boolean",
							Desc: "The third field for Workflow 4",
						},
					},
					Required: []string{"field1", "field2"},
				},
			},
		},
		TopCardData: dashboard.GetTopDashData(),
	}

	r.With(auth.RequireAuth).Get("/", func(w http.ResponseWriter, r *http.Request) {
		handleError(templates.ExecuteTemplate(w, "dashboard.html", mockWorkflows), w, "dashboard/render")
	})

	r.Route("/workflows", func(r chi.Router) {
		r.With(auth.RequireAuth).Get("/", func(w http.ResponseWriter, r *http.Request) {
			handleError(templates.ExecuteTemplate(w, "workflows.html", mockWorkflows), w, "workflows/render")
		})

		r.Post("/create", func(w http.ResponseWriter, r *http.Request) {
			handleError(workflow.Create(w, r, db, runClient, ctx), w, "workflow/create")
		})
	})

	r.With(auth.RequireAPIKey(ctx, db, logger)).Get("/connect/{workflow_name}", func(w http.ResponseWriter, r *http.Request) {
		workflowName := chi.URLParam(r, "workflow_name")
		filter := bson.D{{"name", workflowName}}
		res := db.Database("scavenger").Collection("workflows").FindOne(ctx, filter)
		workflow := workflow.Workflow{}

		if res.Err() != nil {
			handleError(res.Err(), w, "connect")
			return
		}

		err := res.Decode(&workflow)
		if err != nil {
			handleError(err, w, "connect")
			return
		}

		proxy, err := proxyToUri(workflow.ServiceUri)
		if err != nil {
			handleError(err, w, "connect")
			return
		}

		proxy.ServeHTTP(w, r)
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

		r.Get("/api", func(w http.ResponseWriter, r *http.Request) {
			handleError(auth.RenderAPIKey(w, r, templates, nil), w, "api/render")
		})

		r.Post("/api", func(w http.ResponseWriter, r *http.Request) {
			handleError(auth.CreateAPIKey(w, r, db, templates, ctx), w, "api")
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
func proxyToUri(hostString string) (*httputil.ReverseProxy, error) {
	url, err := url.Parse(hostString)
	if err != nil {
		return nil, err
	}
	return httputil.NewSingleHostReverseProxy(url), nil
}
