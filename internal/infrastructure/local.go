package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/ferretcode/scavenger/internal/types"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type LocalServiceProvider struct {
	Config           *types.ScavengerConfig
	logger           *slog.Logger
	db               *mongo.Client
	ctx              context.Context
	dockerClient     *client.Client
	runningWorkflows map[string]string
	mu               sync.Mutex
}

func NewLocalServiceProvider(config *types.ScavengerConfig, db *mongo.Client, ctx context.Context, logger *slog.Logger) (*LocalServiceProvider, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Error("failed to create docker client", "err", err)
		return nil, err
	}

	_, err = cli.Ping(ctx)
	if err != nil {
		logger.Error("failed to ping docker daemon", "err", err)
		return nil, err
	}

	provider := &LocalServiceProvider{
		Config:           config,
		db:               db,
		ctx:              ctx,
		logger:           logger,
		dockerClient:     cli,
		runningWorkflows: make(map[string]string),
		mu:               sync.Mutex{},
	}

	logger.Info("successfully connected to docker daemon")
	logger.Info("attempting to restore running workflows from docker containers...")

	filterArgs := filters.NewArgs()
	filterArgs.Add("status", "running")
	filterArgs.Add("label", "app.scavenger")

	containers, err := cli.ContainerList(ctx, container.ListOptions{Filters: filterArgs})
	if err != nil {
		logger.Error("failed to list docker containers for restoration", "err", err)
		logger.Warn("failed to restore some workflows due to docker list error")
	} else {
		provider.mu.Lock()
		for _, c := range containers {
			workflowName, ok := c.Labels["app.scavenger.workflow"]
			if ok && workflowName != "" {
				provider.runningWorkflows[workflowName] = c.ID
				logger.Info("restored tracking for running workflow", "workflowName", workflowName, "containerID", c.ID)
			} else {
				logger.Warn("found a running container with our label but missing workflow name", "containerID", c.ID, "labels", c.Labels)
			}
		}
		provider.mu.Unlock()
		logger.Info("finished workflow restoration", "restoredCount", len(provider.runningWorkflows))
	}

	return provider, nil
}

func (l *LocalServiceProvider) Close() error {
	if l.dockerClient != nil {
		return l.dockerClient.Close()
	}
	return nil
}

func (l *LocalServiceProvider) CreateWorkflow(w http.ResponseWriter, r *http.Request) error {
	workflow, err := generateWorkflowFromRequest(r)
	if err != nil {
		return err
	}

	imageName := "sthanguy/scavenger-scraper"
	if l.Config.WorkerImage != "" {
		imageName = l.Config.WorkerImage
	}

	schemaBytes, err := json.Marshal(workflow.Schema)
	if err != nil {
		return err
	}

	envVars := []string{
		fmt.Sprintf("CRONTAB=%s", workflow.Cron),
		fmt.Sprintf("GEMINI_API_KEY=%s", l.Config.GeminiApiKey),
		fmt.Sprintf("SCHEMA=%s", string(schemaBytes)),
		fmt.Sprintf("PROMPT=%s", workflow.Prompt),
		fmt.Sprintf("WEBPAGE_URL=%s", workflow.Request.Website),
		fmt.Sprintf("PORT=%s", "8765"),
	}

	containerPort, err := nat.NewPort("tcp", "8765")
	if err != nil {
		l.logger.Error("failed to create nat.Port for 8765/tcp", "err", err)
		return err
	}

	portBindings := nat.PortMap{
		"8765/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: ""}},
	}

	memoryLimitBytes := int64(1.5 * 1024 * 1024 * 1024)

	containerName := fmt.Sprintf("scavenger-workflow-%s", strings.ReplaceAll(workflow.Name, " ", "-"))

	labels := map[string]string{
		"app.scavenger":          "workflow-worker",
		"app.scavenger.workflow": workflow.Name,
	}

	containerConfig := &container.Config{
		Image: imageName,
		Env:   envVars,
		ExposedPorts: nat.PortSet{
			"8765/tcp": struct{}{},
		},
		Labels: labels,
	}

	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		Resources: container.Resources{
			Memory: memoryLimitBytes,
		},
	}

	resp, err := l.dockerClient.ContainerCreate(
		l.ctx,
		containerConfig,
		hostConfig,
		nil,
		nil,
		containerName,
	)
	if err != nil {
		return err
	}

	l.logger.Info("docker container created", "workflow-name", workflow.Name, "container-name", containerName, "container-id", resp.ID)

	err = l.dockerClient.ContainerStart(l.ctx, resp.ID, container.StartOptions{})
	if err != nil {
		l.logger.Error("failed to start docker container", "workflowName", workflow.Name, "container-id", resp.ID, "err", err)

		removeErr := l.dockerClient.ContainerRemove(l.ctx, resp.ID, container.RemoveOptions{Force: true})
		if removeErr != nil {
			l.logger.Error("failed to remove container after start failure", "container-id", resp.ID, "err", removeErr)
		}

		return fmt.Errorf("failed to start docker container for workflow %s (ID: %s): %w", workflow.Name, resp.ID, err)
	}

	l.logger.Info("docker container started", "workflow-name", workflow.Name, "container-id", resp.ID)

	inspectResp, err := l.dockerClient.ContainerInspect(l.ctx, resp.ID)
	if err != nil {
		l.logger.Error("failed to inspect docker container after start", "workflowName", workflow.Name, "container-id", resp.ID, "err", err)
		l.logger.Warn("Attempting to stop/remove container due to inspection failure", "container-id", resp.ID)

		stopTimeout := 10 * time.Second
		timeout := int(stopTimeout.Seconds())
		stopErr := l.dockerClient.ContainerStop(l.ctx, resp.ID, container.StopOptions{Timeout: &timeout})

		if stopErr != nil {
			l.logger.Error("failed to stop container during rollback after inspection failure", "container-id", resp.ID, "err", stopErr)
		}

		removeErr := l.dockerClient.ContainerRemove(l.ctx, resp.ID, container.RemoveOptions{Force: true})
		if removeErr != nil {
			l.logger.Error("failed to remove container during rollback after inspection failure", "container-id", resp.ID, "err", removeErr)
		}

		return err
	}

	bindings, ok := inspectResp.NetworkSettings.Ports[containerPort]
	if !ok || len(bindings) == 0 {
		l.logger.Error("port binding not found for container", "workflowName", workflow.Name, "container-id", resp.ID, "containerPort", containerPort)

		l.logger.Warn("Attempting to stop/remove container due to missing port binding", "container-id", resp.ID)

		stopTimeout := 10 * time.Second
		timeout := int(stopTimeout.Seconds())
		stopErr := l.dockerClient.ContainerStop(l.ctx, resp.ID, container.StopOptions{Timeout: &timeout})

		if stopErr != nil {
			l.logger.Error("failed to stop container during rollback after missing port binding", "container-id", resp.ID, "err", stopErr)
		}
		removeErr := l.dockerClient.ContainerRemove(l.ctx, resp.ID, container.RemoveOptions{Force: true})

		if removeErr != nil {
			l.logger.Error("failed to remove container during rollback after missing port binding", "container-id", resp.ID, "err", removeErr)
		}

		return err
	}

	hostPort := bindings[0].HostPort

	workflow.ServiceUri = fmt.Sprintf("http://localhost:%s", hostPort)

	_, err = l.db.Database(l.Config.DatabaseName).Collection("workflows").InsertOne(l.ctx, workflow)
	if err != nil {
		l.logger.Error("failed to insert workflow into DB after starting container", "workflow-name", workflow.Name, "container-id", resp.ID, "err", err)
		l.logger.Warn("attempting to stop/remove container due to db insertion failure", "container-id", resp.ID)

		stopTimeout := 10
		stopErr := l.dockerClient.ContainerStop(l.ctx, resp.ID, container.StopOptions{Timeout: &stopTimeout})
		if stopErr != nil {
			l.logger.Error("failed to stop container during rollback", "container-id", resp.ID, "err", stopErr)
		}

		removeErr := l.dockerClient.ContainerRemove(l.ctx, resp.ID, container.RemoveOptions{Force: true})
		if removeErr != nil {
			l.logger.Error("failed to remove container during rollback", "container-id", resp.ID, "err", removeErr)
		}

		return fmt.Errorf("failed to save workflow to database after starting container %s: %w", resp.ID, err)
	}

	l.mu.Lock()
	l.runningWorkflows[workflow.Name] = resp.ID
	l.mu.Unlock()

	http.Redirect(w, r, "/", http.StatusSeeOther)

	return nil
}

func (l *LocalServiceProvider) DeleteWorkflow(w http.ResponseWriter, r *http.Request) error {
	err := r.ParseForm()
	if err != nil {
		l.logger.Error("failed to parse form for workflow deletion", "err", err)
		return fmt.Errorf("failed to parse form: %w", err)
	}

	workflowName := r.PostForm.Get("workflowName")
	if workflowName == "" {
		return fmt.Errorf("workflowName is required")
	}

	l.mu.Lock()
	containerID, ok := l.runningWorkflows[workflowName]
	l.mu.Unlock()

	if ok {
		l.logger.Info("stopping docker container for workflow", "workflowName", workflowName, "container-id", containerID)

		stopTimeout := 10 * time.Second
		timeoutSeconds := int(stopTimeout.Seconds())

		stopErr := l.dockerClient.ContainerStop(l.ctx, containerID, container.StopOptions{Timeout: &timeoutSeconds})
		if stopErr != nil {
			l.logger.Error("failed to stop docker container gracefully", "container-id", containerID, "err", stopErr)
		} else {
			l.logger.Info("docker container stopped gracefully", "container-id", containerID)
		}

		l.logger.Info("removing docker container", "workflowName", workflowName, "container-id", containerID)

		removeOptions := container.RemoveOptions{
			RemoveVolumes: true,
			Force:         true,
		}
		removeErr := l.dockerClient.ContainerRemove(l.ctx, containerID, removeOptions)
		if removeErr != nil {
			l.logger.Error("failed to remove docker container", "container-id", containerID, "err", removeErr)
		} else {
			l.logger.Info("successfully removed docker container", "container-id", containerID)
		}

		l.mu.Lock()
		delete(l.runningWorkflows, workflowName)
		l.mu.Unlock()

	} else {
		l.logger.Warn("workflow not found in running workflows map for deletion (container likely not started by this provider instance or already stopped)", "workflowName", workflowName)
	}

	bsonFilter := bson.D{{Key: "name", Value: workflowName}}
	result, dbErr := l.db.Database(l.Config.DatabaseName).Collection("workflows").DeleteOne(l.ctx, bsonFilter)
	if dbErr != nil {
		l.logger.Error("failed to delete workflow from DB", "name", workflowName, "err", dbErr)
		return err
	}

	if result.DeletedCount == 0 {
		l.logger.Warn("workflow not found in DB for deletion", "name", workflowName)
	} else {
		l.logger.Info("workflow deleted from DB", "name", workflowName)
	}

	http.Redirect(w, r, "http://localhost:3000/workflows", http.StatusSeeOther)

	return nil
}

func (l *LocalServiceProvider) GetRunningWorkflows() (int, error) {
	l.mu.Lock()
	count := len(l.runningWorkflows)
	l.mu.Unlock()

	return count, nil
}
