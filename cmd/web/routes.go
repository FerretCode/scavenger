package main

import (
	"context"
	"net/http"

	"github.com/ferretcode/scavenger/internal/auth"
	"github.com/ferretcode/scavenger/internal/dashboard"
	"github.com/ferretcode/scavenger/internal/infrastructure"
	"github.com/ferretcode/scavenger/internal/websocket"
	"github.com/ferretcode/scavenger/pkg/types"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Services struct {
	AuthService      auth.AuthService
	ServiceProvider  infrastructure.ServiceProvider
	WebsocketService websocket.WebsocketService
}

func registerRoutes(
	r chi.Router,
	services Services,
	db *mongo.Client,
	ctx context.Context,
	dashboardCardData *types.DashboardCardData,
) {
	r.With(services.AuthService.RequireAuth).Get("/", func(w http.ResponseWriter, r *http.Request) {
		workflows, err := getWorkflows(db)
		if err != nil {
			handleError(err, w, "index")
			return
		}

		data := dashboard.DashboardData{
			Workflows:   workflows,
			TopCardData: dashboard.GetTopDashData(services.ServiceProvider, ctx),
		}

		data.TopCardData = dashboard.GetTopDashData(services.ServiceProvider, ctx)
		data.TopCardData.DocumentsScraped = dashboardCardData.DocScraped
		data.TopCardData.ClientConnections = dashboardCardData.CliConnects
		handleError(templates.ExecuteTemplate(w, "dashboard.html", data), w, "dashboard/render")
	})

	// healthcheck for startup probe
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	})

	r.Route("/workflows", func(r chi.Router) {
		r.Use(services.AuthService.RequireAuth)

		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			workflows, err := getWorkflows(db)
			if err != nil {
				handleError(err, w, "workflows/fetch")
				return
			}

			data := dashboard.DashboardData{
				Workflows:   workflows,
				TopCardData: dashboard.GetTopDashData(services.ServiceProvider, ctx),
			}

			handleError(templates.ExecuteTemplate(w, "workflows.html", data), w, "workflows/render")
		})

		r.Post("/create", func(w http.ResponseWriter, r *http.Request) {
			handleError(services.ServiceProvider.CreateWorkflow(w, r), w, "workflow/create")
		})

		r.Post("/delete", func(w http.ResponseWriter, r *http.Request) {
			handleError(services.ServiceProvider.DeleteWorkflow(w, r), w, "workflow/delete")
		})
	})

	r.With(services.AuthService.RequireAPIKey(ctx, db, logger, &config)).Get("/connect/{workflow_name}", func(w http.ResponseWriter, r *http.Request) {
		services.WebsocketService.HandleWorkflowConnection(w, r)
	})

	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
			handleError(services.AuthService.RenderLogin(w, r, templates), w, "login/render")
		})

		r.Post("/login", func(w http.ResponseWriter, r *http.Request) {
			handleError(services.AuthService.Login(w, r), w, "login")
		})

		r.Get("/logout", func(w http.ResponseWriter, r *http.Request) {
			handleError(services.AuthService.Logout(w, r), w, "logout")
		})

		r.Route("/api", func(r chi.Router) {
			r.Use(services.AuthService.RequireAuth)

			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				handleError(services.AuthService.RenderAPIKey(w, r, templates, nil), w, "api/render")
			})

			r.Post("/", func(w http.ResponseWriter, r *http.Request) {
				handleError(services.AuthService.CreateAPIKey(w, r, db, templates, ctx), w, "api")
			})
		})
	})
}
