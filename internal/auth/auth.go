package auth

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"net/http"
	"sync"
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

	t := template.Must(template.ParseFiles("login.html"))
	t.ExecuteTemplate(w, "login.html", nil)

	return nil
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
	token, err := generateSessionToken()
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
		return nil
	} else {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:   sessionsCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)

	return nil
}

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func RequireAuth(next http.Handler) http.Handler {
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
