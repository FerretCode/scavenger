package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"cloud.google.com/go/iam/apiv1/iampb"
	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"github.com/ferretcode/scavenger/pkg/types"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const serviceIDCharset = "abcdefghijklmnopqrstuvwxyz0123456789"
const maxServiceIDLength = 49

type GcpServiceProvider struct {
	Config    *types.ScavengerConfig
	logger    *slog.Logger
	db        *mongo.Client
	runClient *run.ServicesClient
	ctx       context.Context
}

func NewGcpServiceProvider(config *types.ScavengerConfig, db *mongo.Client, ctx context.Context, logger *slog.Logger) (*GcpServiceProvider, error) {
	var credentials []byte

	if _, err := os.Stat("./credentials.json"); err == nil {
		bytes, err := os.ReadFile("./credentials.json")
		if err != nil {
			logger.Error("error parsing credentials file", "err", err)
			return nil, err
		}

		credentials = bytes
	} else {
		credentials = []byte(config.GcpCredentialsJson)
	}

	secretManagerClient, err := secretmanager.NewClient(ctx, option.WithCredentialsJSON(credentials))
	if err != nil {
		logger.Error("error using credentials file", "err", err)
		return nil, err
	}
	defer secretManagerClient.Close()
	_ = secretManagerClient

	runClient, err := run.NewServicesClient(ctx, option.WithCredentialsJSON(credentials))
	if err != nil {
		logger.Error("error creating google run client", "err", err)
		return nil, err
	}

	return &GcpServiceProvider{
		Config:    config,
		db:        db,
		ctx:       ctx,
		logger:    logger,
		runClient: runClient,
	}, nil
}

func (g *GcpServiceProvider) DeleteWorkflow(w http.ResponseWriter, r *http.Request) error {
	err := r.ParseForm()
	if err != nil {
		return err
	}

	workflowName := r.PostForm.Get("workflowName")

	workflow := Workflow{}
	workflow.Name = workflowName

	bsonFilter := bson.D{{Key: "workflowName", Value: workflowName}}

	_, err = g.db.Database(os.Getenv("DATABASE_NAME")).Collection("workflows").DeleteOne(g.ctx, bsonFilter)
	if err != nil {
		return err
	}

	http.Redirect(w, r, "http://localhost:3000/workflows", http.StatusSeeOther)

	return nil
}

func (g *GcpServiceProvider) CreateWorkflowFromConfig(workflow Workflow) error {
	schemaString, err := json.Marshal(workflow.Schema)
	if err != nil {
		return err
	}

	err = g.createWorkflow(workflow, string(schemaString))
	if err != nil {
		return err
	}

	return nil
}

func (g *GcpServiceProvider) CreateWorkflow(w http.ResponseWriter, r *http.Request) error {
	workflow, err := generateWorkflowFromRequest(r)
	if err != nil {
		return err
	}

	schemaString, err := json.Marshal(workflow.Schema)
	if err != nil {
		return err
	}

	err = g.createWorkflow(*workflow, string(schemaString))
	if err != nil {
		return err
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)

	return nil
}

func (g *GcpServiceProvider) CheckWorkflowExists(workflowName string) (bool, error) {
	parent := fmt.Sprintf("projects/%s/locations/%s", g.Config.GcpProjectId, g.Config.GcpLocation)

	requestRunPB := &runpb.ListServicesRequest{
		Parent: parent,
	}

	resp := g.runClient.ListServices(g.ctx, requestRunPB)
	done := false

	for !done {
		if service, err := resp.Next(); err != iterator.Done {
			if _, ok := service.Labels[""]; ok {
				return true, nil
			} else {
				return false, ErrNoWorkflowExists
			}
		} else {
			done = true
		}
	}

	return true, nil
}

func (g *GcpServiceProvider) GetRunningWorkflows() (int, error) {

	parent := fmt.Sprintf("projects/%s/locations/%s", g.Config.GcpProjectId, g.Config.GcpLocation)

	requestRunPB := &runpb.ListServicesRequest{
		Parent: parent,
	}

	resp := g.runClient.ListServices(g.ctx, requestRunPB)
	done := false
	totalContainers := 0

	for !done {
		if _, err := resp.Next(); err != iterator.Done {
			totalContainers++
		} else {
			done = true
		}
	}

	return totalContainers, nil
}

func (g *GcpServiceProvider) createWorkflow(workflow Workflow, schemaString string) error {
	exists, err := g.CheckWorkflowExists(workflow.Name)
	if err != nil {
		if err != ErrNoWorkflowExists {
			return err
		}
	}

	if exists {
		return nil // no-op since workflow exists already
	}

	createServiceRequest := &runpb.CreateServiceRequest{
		Parent:    fmt.Sprintf("projects/%s/locations/%s", os.Getenv("GCP_PROJECT_ID"), os.Getenv("GCP_LOCATION")),
		ServiceId: generateServiceID(),
		Service: &runpb.Service{
			Template: &runpb.RevisionTemplate{
				Labels: map[string]string{"workflow": workflow.Name},
				Containers: []*runpb.Container{
					{
						Image: "sthanguy/scavenger-scraper",
						Ports: []*runpb.ContainerPort{
							{
								ContainerPort: 8765, // scraper websocket port
							},
						},
						Resources: &runpb.ResourceRequirements{
							Limits: map[string]string{
								"memory": "1.5 Gi",
							},
						},
						StartupProbe: &runpb.Probe{
							InitialDelaySeconds: 5,
							PeriodSeconds:       2,
							FailureThreshold:    1000,
							ProbeType: &runpb.Probe_HttpGet{
								HttpGet: &runpb.HTTPGetAction{
									Path: "/healthz",
									Port: 8765,
								},
							},
						},
						Env: []*runpb.EnvVar{
							{
								Name: "CRONTAB",
								Values: &runpb.EnvVar_Value{
									Value: workflow.Cron,
								},
							},
							{
								Name: "GEMINI_API_KEY",
								Values: &runpb.EnvVar_Value{
									Value: os.Getenv("GEMINI_API_KEY"),
								},
							},
							{
								Name: "SCHEMA",
								Values: &runpb.EnvVar_Value{
									Value: string(schemaString),
								},
							},
							{
								Name: "PROMPT",
								Values: &runpb.EnvVar_Value{
									Value: workflow.Prompt,
								},
							},
							{
								Name: "WEBPAGE_URL",
								Values: &runpb.EnvVar_Value{
									Value: workflow.Request.Website,
								},
							},
						},
					},
				},
			},
		},
	}

	resp, err := g.runClient.CreateService(g.ctx, createServiceRequest)
	if err != nil {
		return err
	}

	service, err := resp.Wait(g.ctx)
	if err != nil {
		return err
	}

	resource := fmt.Sprintf("projects/%s/locations/%s/services/%s", os.Getenv("GCP_PROJECT_ID"), os.Getenv("GCP_LOCATION"), createServiceRequest.ServiceId)

	policy, err := g.runClient.GetIamPolicy(g.ctx, &iampb.GetIamPolicyRequest{
		Resource: resource,
	})
	if err != nil {
		return err
	}

	policy.Bindings = append(policy.Bindings, &iampb.Binding{
		Role:    "roles/run.invoker",
		Members: []string{"allUsers"},
	})

	_, err = g.runClient.SetIamPolicy(g.ctx, &iampb.SetIamPolicyRequest{
		Resource: resource,
		Policy:   policy,
	})
	if err != nil {
		return err
	}

	workflow.ServiceUri = service.Uri

	_, err = g.db.Database(g.Config.DatabaseName).Collection("workflows").InsertOne(g.ctx, workflow)
	if err != nil {
		return err
	}

	return nil
}
