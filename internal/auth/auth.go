package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
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

type ApiKey struct {
	Hash string `json:"hash"`
}

func RenderLogin(w http.ResponseWriter, r *http.Request, templates *template.Template) error {
	cookie, err := r.Cookie(sessionsCookieName)
	if err == nil {
		sessionsMu.Lock()
		valid := sessions[cookie.Value]
		sessionsMu.Unlock()

		if valid {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return nil
		}
	}

	return templates.ExecuteTemplate(w, "login.html", nil)
}

func Login(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return nil
	}

	// Check credentials
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username != adminUsername || password != adminPassword {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return nil
	}

	// Generate session token for authenticated user
	token, err := generateToken()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil
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

	return nil
}

func Logout(w http.ResponseWriter, r *http.Request) error {
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

	http.Redirect(w, r, "/", http.StatusSeeOther)

	return nil
}

func RenderAPIKey(w http.ResponseWriter, r *http.Request, templates *template.Template, data any) error {
	return templates.ExecuteTemplate(w, "api.html", data)
}

func CreateAPIKey(w http.ResponseWriter, r *http.Request, db *mongo.Client, templates *template.Template, ctx context.Context) error {
	t, err := generateToken()
	if err != nil {
		return err
	}

	b := hashToken(t)
	encoded := base64.StdEncoding.EncodeToString(b)

	apiKey := ApiKey{
		Hash: encoded,
	}

	_, err = db.Database(os.Getenv("DATABASE_NAME")).Collection("api_keys").InsertOne(ctx, apiKey)
	if err != nil {
		return err
	}

	return RenderAPIKey(w, r, templates, t)
}

// Generates a plaintext token
func generateToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashToken(plaintextToken string) []byte {
	hash := sha256.Sum256([]byte(plaintextToken))
	return hash[:]
}

func RequireAPIKey(ctx context.Context, db *mongo.Client, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")

			hash := hashToken(apiKey)

			filter := bson.D{{"hash", hash}}

			res := db.Database("scavenger").Collection("api_keys").FindOne(ctx, filter)

			if res.Err() != nil {
				logger.Error("error fetching api key from database", "err", res.Err())
				http.Error(w, "error authenticating your request", http.StatusInternalServerError)
				return
			}

			key := ApiKey{}
			fmt.Println(key.Hash)

			fmt.Printf("%T\n", key.Hash)

			err := res.Decode(&key)
			if err != nil {
				logger.Error("error fetching api key from database", "err", res.Err())
				http.Error(w, "error authenticating your request", http.StatusInternalServerError)
				return
			}

			comparisonHash, err := base64.StdEncoding.DecodeString(key.Hash)
			if err != nil {
				logger.Error("error fetching api key from database", "err", res.Err())
				http.Error(w, "error authenticating your request", http.StatusInternalServerError)
				return
			}

			comparisonResult := subtle.ConstantTimeCompare(hash, comparisonHash)
			if comparisonResult != 0 {
				http.Error(w, "incorrect api key", http.StatusUnauthorized)
				return
			}
		})
	}
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionsCookieName)
		if err != nil {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		sessionsMu.Lock()
		valid := sessions[cookie.Value]
		sessionsMu.Unlock()

		if !valid {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}
