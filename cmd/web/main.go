package main

import (
	"context"
	"html/template"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"time"

	run "cloud.google.com/go/run/apiv2"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"github.com/ferretcode/scavenger/internal/auth"
	"github.com/ferretcode/scavenger/internal/dashboard"
	"github.com/ferretcode/scavenger/internal/workflow"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
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

	// Retrieve all the workflows
	cur, err := db.Database("scavenger").Collection("workflows").Find(ctx, bson.D{{}})
	if err != nil {
		logger.Error("error retrieving workflows", "err", err)
		return
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

	if err := cur.Err(); err != nil {
		logger.Error("error parsing workflows", "err", err)
		return
	}

	// CODE STARTS HERE
	r := chi.NewRouter()

	mockWorkflows := dashboard.DashboardData{
		Workflows:   workflows,
		TopCardData: dashboard.GetTopDashData(),
	}

	type DashboardLastTwo struct {
		DocScraped  int
		CliConnects int
	}

	lastTwoCards := DashboardLastTwo{
		DocScraped:  0,
		CliConnects: 0,
	}

	r.With(auth.RequireAuth).Get("/", func(w http.ResponseWriter, r *http.Request) {
		mockWorkflows.TopCardData = dashboard.GetTopDashData(runClient, ctx)
		mockWorkflows.TopCardData.DocumentsScraped = lastTwoCards.DocScraped
		mockWorkflows.TopCardData.ClientConnections = lastTwoCards.CliConnects
		handleError(templates.ExecuteTemplate(w, "dashboard.html", mockWorkflows), w, "dashboard/render")
	})

	r.Route("/workflows", func(r chi.Router) {
		r.Use(auth.RequireAuth)

		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			handleError(templates.ExecuteTemplate(w, "workflows.html", mockWorkflows), w, "workflows/render")
		})

		r.Post("/create", func(w http.ResponseWriter, r *http.Request) {
			handleError(workflow.Create(w, r, db, runClient, ctx), w, "workflow/create")

		})

		r.Post("/delete", func(w http.ResponseWriter, r *http.Request) {
			handleError(workflow.Delete(w, r, db, runClient, ctx), w, "workflow/delete")
		})

	})

	r.With(auth.RequireAPIKey(ctx, db, logger)).Get("/connect/{workflow_name}", func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}

		clientConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			handleError(err, w, "connect/upgrade")
			return
		}

		workflowName := chi.URLParam(r, "workflow_name")
		filter := bson.D{{"name", workflowName}}
		res := db.Database("scavenger").Collection("workflows").FindOne(ctx, filter)
		workflow := workflow.Workflow{}

		if res.Err() != nil {
			clientConn.Close()
			handleError(res.Err(), w, "connect/find")
			return
		}

		err = res.Decode(&workflow)
		if err != nil {
			clientConn.Close()
			handleError(err, w, "connect/decode")
			return
		}

		serviceUri, err := url.Parse(workflow.ServiceUri)
		if err != nil {
			clientConn.Close()
			handleError(err, w, "connect/service")
			return
		}

		if serviceUri.Scheme == "https" {
			serviceUri.Scheme = "wss"
		} else {
			serviceUri.Scheme = "ws"
		}

		targetUri := serviceUri.String() + "/ws"

		dialer := websocket.DefaultDialer

		serverConn, resp, err := dialer.Dial(targetUri, nil)
		if err != nil {
			clientConn.Close()
			if resp != nil {
				body, readErr := io.ReadAll(resp.Body)
				if readErr != nil {
					logger.Error("handshake failed", "status", resp.StatusCode, "err", readErr)
				} else {
					logger.Error("handshake failed", "status", resp.StatusCode, "body", string(body))
				}
			}
			handleError(err, w, "connect/connection")
			return
		}

		lastTwoCards.DocScraped++
		lastTwoCards.CliConnects++

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			defer cancel()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					mt, message, err := clientConn.ReadMessage()
					if err != nil {
						logger.Error("read from client failed", "err", err)
						return
					}

					err = serverConn.WriteMessage(mt, message)
					if err != nil {
						logger.Error("write to server failed", "err", err)
						return
					}
				}
			}
		}()

		go func() {
			defer wg.Done()
			defer cancel()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					mt, message, err := serverConn.ReadMessage()
					if err != nil {
						logger.Error("read from server failed", "err", err)
						return
					}

					err = clientConn.WriteMessage(mt, message)
					if err != nil {
						logger.Error("write to client failed", "err", err)
						return
					}
				}
			}
		}()

		<-ctx.Done()

		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "connection closed")
		clientConn.WriteMessage(websocket.CloseMessage, closeMsg)
		serverConn.WriteMessage(websocket.CloseMessage, closeMsg)

		clientConn.Close()
		serverConn.Close()

		wg.Wait()

		lastTwoCards.DocScraped--
		lastTwoCards.CliConnects--
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

		r.Route("/api", func(r chi.Router) {
			r.Use(auth.RequireAuth)

			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				handleError(auth.RenderAPIKey(w, r, templates, nil), w, "api/render")
			})

			r.Post("/", func(w http.ResponseWriter, r *http.Request) {
				handleError(auth.CreateAPIKey(w, r, db, templates, ctx), w, "api")
			})
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
