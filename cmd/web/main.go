package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"sync"
	"text/template"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const (
	sessionsCookieName = "sessions"
	adminUsername      = "admin"
	adminPassword      = "password"
)

var (
	sessionsMu sync.Mutex
	sessions   = make(map[string]bool) // map[sessionToken]isAuthenticated
)

func main() {
	// Read environemnt
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

	r.With(requireAuth).Get("/", func(w http.ResponseWriter, r *http.Request) {
		t := template.Must(template.ParseFiles("views/index.html"))
		t.Execute(w, nil)
	})

	r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
		// Check if user is already authenticated
		cookie, err := r.Cookie(sessionsCookieName)
		if err == nil {
			sessionsMu.Lock()
			valid := sessions[cookie.Value]
			sessionsMu.Unlock()

			if valid {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}

		t := template.Must(template.ParseFiles("views/login.html"))
		t.Execute(w, nil)
	})

	r.Post("/login", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Check credentials
		username := r.FormValue("username")
		password := r.FormValue("password")
		if username != adminUsername || password != adminPassword {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		// Generate session token for authenticated user
		token, err := generateSessionToken()
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		sessionsMu.Lock()
		sessions[token] = true
		sessionsMu.Unlock()

		http.SetCookie(w, &http.Cookie{
			Name:     sessionsCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   false, // change to true if using HTTPS
		})

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	r.Get("/logout", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionsCookieName)
		if err == nil {
			sessionsMu.Lock()
			delete(sessions, cookie.Value)
			sessionsMu.Unlock()
		}

		http.SetCookie(w, &http.Cookie{
			Name:   sessionsCookieName,
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})

		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})

	log.Println("Running web server http://localhost:3000")
	err = http.ListenAndServe(":3000", r)
	if err != nil {
		log.Fatal(err)
	}
}

func generateSessionToken() (string, error) {
	b := make([]byte, 32)

	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionsCookieName)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		sessionsMu.Lock()
		valid := sessions[cookie.Value]
		sessionsMu.Unlock()

		if !valid {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}
