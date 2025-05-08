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
	"sync"

	"github.com/ferretcode/scavenger/internal/types"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var (
	sessionsMu sync.Mutex
	sessions   = make(map[string]bool) // map[sessionToken]isAuthenticated
)

type AuthService struct {
	Config *types.ScavengerConfig
}

type ApiKey struct {
	Hash string `json:"hash"`
}

func NewAuthService(config *types.ScavengerConfig) AuthService {
	return AuthService{
		Config: config,
	}
}

func (a *AuthService) RenderLogin(w http.ResponseWriter, r *http.Request, templates *template.Template) error {
	cookie, err := r.Cookie(a.Config.SessionsCookieName)
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

func (a *AuthService) Login(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return nil
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username != a.Config.AdminUsername || password != a.Config.AdminPassword {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return nil
	}

	token, err := generateToken()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil
	}

	sessionsMu.Lock()
	sessions[token] = true
	sessionsMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     a.Config.SessionsCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
	})

	fmt.Println("logged in")

	http.Redirect(w, r, "/", http.StatusSeeOther)

	return nil
}

func (a *AuthService) Logout(w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(a.Config.SessionsCookieName)
	if err == nil {
		sessionsMu.Lock()
		delete(sessions, cookie.Value)
		sessionsMu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:   a.Config.SessionsCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)

	return nil
}

func (a *AuthService) RenderAPIKey(w http.ResponseWriter, r *http.Request, templates *template.Template, data any) error {
	return templates.ExecuteTemplate(w, "api.html", data)
}

func (a *AuthService) CreateAPIKey(w http.ResponseWriter, r *http.Request, db *mongo.Client, templates *template.Template, ctx context.Context) error {
	t, err := generateToken()
	if err != nil {
		return err
	}

	b := hashToken(t)
	encoded := base64.StdEncoding.EncodeToString(b)

	apiKey := ApiKey{
		Hash: encoded,
	}

	_, err = db.Database(a.Config.DatabaseName).Collection("api_keys").InsertOne(ctx, apiKey)
	if err != nil {
		return err
	}

	return a.RenderAPIKey(w, r, templates, t)
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

func (a *AuthService) RequireAPIKey(ctx context.Context, db *mongo.Client, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")

			hash := hashToken(apiKey)
			encoded := base64.StdEncoding.EncodeToString(hash)
			filter := bson.D{{"hash", encoded}}

			res := db.Database(a.Config.DatabaseName).Collection("api_keys").FindOne(ctx, filter)

			if res.Err() != nil {
				if res.Err() == mongo.ErrNoDocuments {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}

				logger.Error("error fetching api key from database", "err", res.Err())
				http.Error(w, "error authenticating your request", http.StatusInternalServerError)
				return
			}

			key := ApiKey{}

			err := res.Decode(&key)
			if err != nil {
				logger.Error("error fetching api key from database", "err", err)
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
			if comparisonResult != 1 {
				http.Error(w, "incorrect api key", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (a *AuthService) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(a.Config.SessionsCookieName)
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
