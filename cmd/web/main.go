package main

import (
	"context"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"text/template"
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

	r.Route("/workflow", func(r chi.Router) {
		r.Post("/create", func(w http.ResponseWriter, r *http.Request) {

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
		log.Fatal(err)
	}
}

func handleError(err error, w http.ResponseWriter, svc string) {
	if err != nil {
		http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
		logger.Error("error processing request", "svc", svc, "err", err)
	}
}

//taking the host req
//creatign a reverse proxy using httputil
func connectingHostToUser(hostString string)(*httputil.ReverseProxy,error){
	url, err := url.Parse(hostString)
	if err != nil{
		return nil, err
	}
	return httputil.NewSingleHostReverseProxy(url), nil
}

func proxyRequestHandler(proxy *httputil.ReverseProxy) func(http.ResponseWriter, *http.Request){
		return func(w http.ResponseWriter, r *http.Request){
				proxy.ServeHTTP(w,r)
		}		
}
