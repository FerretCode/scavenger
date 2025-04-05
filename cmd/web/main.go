package main

import (
	"context"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ferretcode/scavenger/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

var templates *template.Template
var logger *slog.Logger

func parseTemplates() error {
	var err error

	files := []string{
		"./views/index.html",
		"./views/error.html",
	}

	templates, err = template.ParseFiles(files...)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("missing env: DATABASE_URL")
	}

	// Connect to MongoDB
	client, err := mongo.Connect(options.Client().ApplyURI(dsn))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err = client.Disconnect(context.Background()); err != nil {
			panic(err)
		}
	}()

	// Ping the database
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = client.Ping(ctx, readpref.Primary())

	r := chi.NewRouter()

	r.With(auth.RequireAuth).Get("/", func(w http.ResponseWriter, r *http.Request) {
		t := template.Must(template.ParseFiles("views/index.html"))
		t.Execute(w, nil)
	})

	r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
		handleError(auth.RenderLogin(w, r, templates), w, "login/render")
	})

	r.Post("/login", func(w http.ResponseWriter, r *http.Request) {
		handleError(auth.Login(w, r), w, "login")
	})

	r.Get("/logout", func(w http.ResponseWriter, r *http.Request) {
		handleError(auth.Logout(w, r), w, "logout")
	})

	log.Println("Running web server http://localhost:3000")
	err = http.ListenAndServe(":3000", r)
	if err != nil {
		log.Fatal(err)
	}
}

func handleError(err error, w http.ResponseWriter, svc string) {
	if err != nil {
		http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
		logger.Error("error processing request", "svc", svc, "err", err)
	}
}
