package dashboard

import (
	"context"

	"github.com/ferretcode/scavenger/internal/infrastructure"
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

func GetTopDashData(serviceProvider infrastructure.ServiceProvider, ctx context.Context) TopDashData {
	running, err := serviceProvider.GetRunningWorkflows()
	if err != nil {
		running = 0 // or handle error
	}

	dashboardDataObj := TopDashData{
		RunningWorkflows:  running,
		DocumentsScraped:  0,
		ClientConnections: 0,
	}

	return dashboardDataObj
}
