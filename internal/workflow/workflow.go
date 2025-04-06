package workflow

import (
	"fmt"
	"net/http"
	"os"

	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func Create(w http.ResponseWriter, r *http.Request, db *mongo.Client, runClient *run.ServicesClient) error {
	// TODO: perform database insertion

	createServiceRequest := &runpb.CreateServiceRequest{
		Parent:    fmt.Sprintf("projects/%s/location/%s", os.Getenv("PROJECT_ID"), os.Getenv("GCP_LOCATION")),
		ServiceId: uuid.NewString(),
		Service: &runpb.Service{
			Template: &runpb.RevisionTemplate{
				Containers: []*runpb.Container{
					{
						Image: "sthanguy/scavenger-scraper",
						Ports: []*runpb.ContainerPort{
							{
								ContainerPort: 8765, // scraper websocket port
							},
						},
						Env: []*runpb.EnvVar{
							{
								Name: "CRONTAB",
								Values: &runpb.EnvVar_Value{
									Value: "test",
								},
							},
							{
								Name: "GEMINI_API_KEY",
								Values: &runpb.EnvVar_Value{
									Value: "test",
								},
							},
							{
								Name: "SCHEMA",
								Values: &runpb.EnvVar_Value{
									Value: "test",
								},
							},
							{
								Name: "PROMPT",
								Values: &runpb.EnvVar_Value{
									Value: "test",
								},
							},
							{
								Name: "WEBPAGE_URL",
								Values: &runpb.EnvVar_Value{
									Value: "test",
								},
							},
						},
					},
				},
			},
		},
	}

	resp, err := runClient

	return nil
}
