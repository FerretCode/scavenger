package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"text/template"

	"github.com/go-chi/chi/v5"
)

const (
	adminUsername = "admin"
	adminPassword = "password"
)

var (
	sessionsMu sync.Mutex
	sessions   = make(map[string]bool) // map[sessionToken]isAuthenticated
)

func main() {
	r := chi.NewRouter()

	r.With(requireAuth).Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Scavenger"))
	})

	r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
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

		//race conditions
		sessionsMu.Lock()
		sessions[token] = true
		sessionsMu.Unlock()

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   false, // change to true if using HTTPS
		})

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	r.Get("/logout", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err == nil {
			sessionsMu.Lock()
			delete(sessions, cookie.Value)
			sessionsMu.Unlock()
		}

		http.SetCookie(w, &http.Cookie{
			Name:   "session",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})

		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})

	log.Println("Running web server http://localhost:3000")
	err := http.ListenAndServe(":3000", r)
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
		cookie, err := r.Cookie("session")
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
