package websocket

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"

	"github.com/ferretcode/scavenger/internal/workflow"
	"github.com/ferretcode/scavenger/pkg/types"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type WebsocketService struct {
	Config *types.ScavengerConfig
	db     *mongo.Client
	logger *slog.Logger
	ctx    context.Context

	dashboardCardData *types.DashboardCardData
}

func NewWebsocketService(
	config *types.ScavengerConfig,
	db *mongo.Client,
	logger *slog.Logger,
	ctx context.Context,
	dashboardCardData *types.DashboardCardData,
) WebsocketService {
	return WebsocketService{
		Config:            config,
		db:                db,
		logger:            logger,
		ctx:               ctx,
		dashboardCardData: dashboardCardData,
	}
}

func (ws *WebsocketService) HandleWorkflowConnection(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		handleError(err, w, "connect/upgrade", ws.logger)
		return
	}

	workflowName := chi.URLParam(r, "workflow_name")
	filter := bson.D{{"name", workflowName}}
	res := ws.db.Database("scavenger").Collection("workflows").FindOne(ws.ctx, filter)
	workflow := workflow.Workflow{}

	if res.Err() != nil {
		clientConn.Close()
		handleError(res.Err(), w, "connect/find", ws.logger)
		return
	}

	err = res.Decode(&workflow)
	if err != nil {
		clientConn.Close()
		handleError(err, w, "connect/decode", ws.logger)
		return
	}

	serviceUri, err := url.Parse(workflow.ServiceUri)
	if err != nil {
		clientConn.Close()
		handleError(err, w, "connect/service", ws.logger)
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
				ws.logger.Error("handshake failed", "status", resp.StatusCode, "err", readErr)
			} else {
				ws.logger.Error("handshake failed", "status", resp.StatusCode, "body", string(body))
			}
		}
		handleError(err, w, "connect/connection", ws.logger)
		return
	}

	ws.dashboardCardData.CliConnects++

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
					ws.logger.Error("read from client failed", "err", err)
					return
				}

				err = serverConn.WriteMessage(mt, message)
				if err != nil {
					ws.logger.Error("write to server failed", "err", err)
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
					ws.logger.Error("read from server failed", "err", err)
					return
				}

				ws.dashboardCardData.DocScraped++

				err = clientConn.WriteMessage(mt, message)
				if err != nil {
					ws.logger.Error("write to client failed", "err", err)
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

	ws.dashboardCardData.CliConnects--
}

func handleError(err error, w http.ResponseWriter, svc string, logger *slog.Logger) {
	if err != nil {
		http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
		logger.Error("error processing request", "svc", svc, "err", err)
	}
}
