package bootstrap

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"

	"github.com/ferretcode/scavenger/internal/infrastructure"
	"github.com/ferretcode/scavenger/pkg/types"
)

func Bootstrap(serviceProvider infrastructure.ServiceProvider, logger *slog.Logger) []error {
	var errors []error

	configBytes, err := os.ReadFile("./config.json")
	if err != nil {
		errors = append(errors, err)
		return errors
	}

	var workflows []types.WorkflowsConfig

	if err := json.Unmarshal(configBytes, &workflows); err != nil {
		errors = append(errors, err)
		return errors
	}

	logger.Info("found workflows in configuration", "num", len(workflows))

	for _, workflow := range workflows {
		logger.Info("generating workflow from configuration", "name", workflow.Name)

		schema := infrastructure.Schema{
			Type:       "object",
			Title:      "Generated Schema",
			Properties: make(map[string]infrastructure.Field),
		}

		numKeys := 0
		for key, field := range workflow.Schema {
			schema.Properties[key] = infrastructure.Field{
				Name: field.Name,
				Type: field.Type,
				Desc: field.Desc,
			}
			schema.Required = append(schema.Required, key)
			numKeys++
		}

		if numKeys == 0 {
			logger.Warn("no schema fields found for workflow, skipping", "workflow-name", workflow.Name)
			continue
		}

		workflowName := strings.ReplaceAll(strings.ToLower(workflow.Name), " ", "_")

		serviceProviderWorkflow := infrastructure.Workflow{
			Name:   workflowName,
			Prompt: workflow.Prompt,
			Schema: schema,
			Cron:   workflow.Cron,
			Request: infrastructure.WorkflowRequestContext{
				WorkflowName: workflowName,
				Website:      workflow.Website,
				Prompt:       workflow.Prompt,
				NumberFields: numKeys,
			},
		}

		err = serviceProvider.CreateWorkflowFromConfig(serviceProviderWorkflow)
		if err != nil {
			logger.Error("failed to create workflow", "workflow-name", workflowName, "err", err)
			errors = append(errors, err)
			continue
		}
	}

	return errors
}
