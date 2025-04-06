package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"

	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const serviceIDCharset = "abcdefghijklmnopqrstuvwxyz0123456789"
const maxServiceIDLength = 49

type Field struct {
	Name string `json:"title"`
	Type string `json:"type"`
	Desc string `json:"description"`
}

type Schema struct {
	Properties map[string]Field `json:"properties"`
	Required   []string         `json:"required"`
	Title      string           `json:"title"`
	Type       string           `json:"type"`
}

func Create(w http.ResponseWriter, r *http.Request, db *mongo.Client, runClient *run.ServicesClient, ctx *context.Context) error {
	// TODO: perform database insertion
	err := r.ParseForm()
	if err != nil {
		return err
	}

	fmt.Println("All form data:", r.PostForm)
	fmt.Println("All form data:", r.Form)

	website := r.PostForm.Get("websiteInput")
	cron := r.PostForm.Get("cronInput")
	prompt := r.PostForm.Get("promptInput")
	numberFields := r.PostForm.Get("numberFields")

	fieldCounter, err := strconv.Atoi(numberFields)
	if err != nil {
		return err
	}

	schema := Schema{
		Type:       "object",
		Title:      "Generated Schema",
		Properties: make(map[string]Field),
	}

	for i := 0; i < fieldCounter; i++ {
		fieldName := r.PostForm.Get(fmt.Sprintf("fieldName_%d", i))
		fieldType := r.PostForm.Get(fmt.Sprintf("fieldType_%d", i))
		fieldDesc := r.PostForm.Get(fmt.Sprintf("fieldDesc_%d", i))

		if fieldName == "" || fieldType == "" || fieldDesc == "" {
			// the field was deleted
			// the field counter is never udpated when a field is deleted
			continue
		}

		field := Field{
			Name: fieldName,
			Type: fieldType,
			Desc: fieldDesc,
		}

		schemaFieldName := strings.ReplaceAll(strings.ToLower(fieldName), " ", "_")

		schema.Properties[schemaFieldName] = field
		schema.Required = append(schema.Required, schemaFieldName)
	}

	schemaString, err := json.Marshal(schema)
	if err != nil {
		return err
	}

	createServiceRequest := &runpb.CreateServiceRequest{
		Parent:    fmt.Sprintf("projects/%s/locations/%s", os.Getenv("GCP_PROJECT_ID"), os.Getenv("GCP_LOCATION")),
		ServiceId: generateServiceID(),
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
									Value: cron,
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
									Value: prompt,
								},
							},
							{
								Name: "WEBPAGE_URL",
								Values: &runpb.EnvVar_Value{
									Value: website,
								},
							},
						},
					},
				},
			},
		},
	}

	resp, err := runClient.CreateService(*ctx, createServiceRequest)
	if err != nil {
		return err
	}

	service, err := resp.Wait(*ctx)
	if err != nil {
		return err
	}

	fmt.Println(service.Uri)

	return nil
}

func generateServiceID() string {
	length := 20 + rand.Intn(maxServiceIDLength-20)

	var sb strings.Builder

	sb.WriteByte('a' + byte(rand.Intn(26)))

	for i := 1; i < length-1; i++ {
		if rand.Float64() < 0.15 {
			sb.WriteByte('-')
		} else {
			sb.WriteByte(serviceIDCharset[rand.Intn(len(serviceIDCharset))])
		}
	}

	for {
		lastChar := serviceIDCharset[rand.Intn(len(serviceIDCharset))]
		if lastChar != '-' {
			sb.WriteByte(lastChar)
			break
		}
	}

	return sb.String()
}
