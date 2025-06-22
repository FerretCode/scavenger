package infrastructure

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
)

var ErrNoWorkflowExists = errors.New("this workflow does not exist")

type ServiceProvider interface {
	CreateWorkflow(w http.ResponseWriter, r *http.Request) error
	CreateWorkflowFromConfig(workflow Workflow) error
	DeleteWorkflow(w http.ResponseWriter, r *http.Request) error
	CheckWorkflowExists(workflowName string) (bool, error)
	GetRunningWorkflows() (int, error)
}

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

type WorkflowRequestContext struct {
	WorkflowName string `json:"workflow_name"`
	Website      string `json:"website"`
	Cron         string `json:"cron"`
	Prompt       string `json:"prompt"`
	NumberFields int    `json:"number_fields"`
}

type Workflow struct {
	Name       string                 `json:"name"`
	ServiceUri string                 `json:"service_uri"`
	Prompt     string                 `json:"prompt"`
	Cron       string                 `json:"cron"`
	Schema     Schema                 `json:"schema"`
	Request    WorkflowRequestContext `json:"request"`
}

func generateWorkflowFromRequest(r *http.Request) (*Workflow, error) {
	err := r.ParseForm()
	if err != nil {
		return nil, err
	}

	workflowName := r.PostForm.Get("nameInput")
	website := r.PostForm.Get("websiteInput")
	cron := r.PostForm.Get("cronInput")
	prompt := r.PostForm.Get("promptInput")
	numberFields := r.PostForm.Get("numberFields")

	fieldCounter, err := strconv.Atoi(numberFields)
	if err != nil {
		return nil, err
	}

	schema := Schema{
		Type:       "object",
		Title:      "Generated Schema",
		Properties: make(map[string]Field),
	}

	for i := 0; i <= fieldCounter; i++ {
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

		schemaFieldName := strings.ReplaceAll(strings.ToLower(fieldName), " ", "-")

		schema.Properties[schemaFieldName] = field
		schema.Required = append(schema.Required, schemaFieldName)
	}

	workflowName = strings.ReplaceAll(strings.ToLower(workflowName), " ", "_")
	workflow := Workflow{
		Name:   workflowName,
		Prompt: prompt,
		Schema: schema,
		Cron:   cron,
		Request: WorkflowRequestContext{
			WorkflowName: workflowName,
			Website:      website,
			Cron:         cron,
			Prompt:       prompt,
			NumberFields: fieldCounter,
		},
	}

	return &workflow, nil
}

func generateServiceID() string {
	length := 20 + rand.Intn(maxServiceIDLength-20)

	var sb strings.Builder

	sb.WriteByte('a' + byte(rand.Intn(26)))

	for i := 1; i < length-1; i++ {
		if rand.Float64() < 0.15 && sb.String()[i-1] != '-' {
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
