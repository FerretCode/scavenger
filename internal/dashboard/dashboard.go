package dashboard

import (
	"github.com/ferretcode/scavenger/internal/workflow"
)

type TopDashData struct {
	RunningWorkflows  int
	DocumentsScraped  int
	ClientConnections int
}

type DashboardData struct {
	Workflows   []workflow.Workflow
	TopCardData TopDashData
}

func GetTopDashData() TopDashData {
	running, err := workflow.GetRunningWorkflows()
	if err != nil {
		running = 0 // or handle error
	}

	documents, err := workflow.GetDocumentScraped()
	if err != nil {
		documents = 0
	}

	clients, err := workflow.GetActiveClients()
	if err != nil {
		clients = 0
	}

	dashboardDataObj := TopDashData{
		RunningWorkflows:  running,
		DocumentsScraped:  documents,
		ClientConnections: clients,
	}

	return dashboardDataObj
}
