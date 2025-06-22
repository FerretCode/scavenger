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
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/ferretcode/scavenger/internal/auth"
	"github.com/ferretcode/scavenger/internal/bootstrap"
	"github.com/ferretcode/scavenger/internal/infrastructure"
	"github.com/ferretcode/scavenger/internal/websocket"
	"github.com/ferretcode/scavenger/internal/workflow"
	"github.com/ferretcode/scavenger/pkg/types"
	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

var templates *template.Template
var logger *slog.Logger
var config types.ScavengerConfig

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

	if _, err := os.Stat(".env"); err == nil {
		err := godotenv.Load()
		if err != nil {
			logger.Error("error parsing .env", "err", err)
			return
		}
	}

	if err := env.Parse(&config); err != nil {
		logger.Error("error parsing config", "err", err)
		return
	}

	err := parseTemplates()
	if err != nil {
		logger.Error("error parsing templates", "err", err)
		return
	}

	dsn := config.DatabaseUrl
	if dsn == "" {
		logger.Error("database url does not exist in the environment variables")
		return
	}

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

	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = db.Ping(pingCtx, readpref.Primary())

	dashboardCardData := types.DashboardCardData{
		DocScraped:  0,
		CliConnects: 0,
	}

	r := chi.NewRouter()

	authService := auth.NewAuthService(&config)
	websocketService := websocket.NewWebsocketService(&config, db, logger, ctx, &dashboardCardData)
	var serviceProvider infrastructure.ServiceProvider

	switch strings.ToLower(config.Provider) {
	case "gcp":
		serviceProvider, err = infrastructure.NewGcpServiceProvider(&config, db, ctx, logger)
		if err != nil {
			logger.Error("error initializing gcp provider", "err", err)
			return
		}
		break
	case "local":
		localServiceProvider, err := infrastructure.NewLocalServiceProvider(&config, db, ctx, logger)
		if err != nil {
			logger.Error("error initializing local provider", "err", err)
			return
		}
		defer localServiceProvider.Close()
		serviceProvider = localServiceProvider

		errors := bootstrap.Bootstrap(serviceProvider, logger)
		for _, err := range errors {
			if err != nil {
				logger.Error("error bootstrapping workflows from configuration", "err", err)
				return
			}
		}

		break
	default:
		logger.Error("error selecting provider. invalid provider provided")
	}

	registerRoutes(
		r,
		Services{
			AuthService:      authService,
			ServiceProvider:  serviceProvider,
			WebsocketService: websocketService,
		},
		db,
		ctx,
		&dashboardCardData,
	)

	log.Println("Running web server http://localhost:3000")
	err = http.ListenAndServe(":3000", r)
	if err != nil {
		logger.Error("error serving http server", "error", err)
		return
	}
}

func getWorkflows(db *mongo.Client) ([]workflow.Workflow, error) {
	cur, err := db.Database(config.DatabaseName).Collection("workflows").Find(context.Background(), bson.D{{}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.Background())

	var workflows []workflow.Workflow
	for cur.Next(context.Background()) {
		var workflow workflow.Workflow
		if err := cur.Decode(&workflow); err != nil {
			log.Fatal(err)
		}
		workflows = append(workflows, workflow)
	}

	return workflows, cur.Err()
}

func handleError(err error, w http.ResponseWriter, svc string) {
	if err != nil {
		http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
		logger.Error("error processing request", "svc", svc, "err", err)
	}
}

func proxyToUri(hostString string) (*httputil.ReverseProxy, error) {
	url, err := url.Parse(hostString)
	if err != nil {
		return nil, err
	}
	return httputil.NewSingleHostReverseProxy(url), nil
}
