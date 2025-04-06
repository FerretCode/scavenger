package dashboard

import (
	"context"

	run "cloud.google.com/go/run/apiv2"
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

func GetTopDashData(runClient *run.ServicesClient, ctx context.Context) TopDashData {
	running, err := workflow.GetRunningWorkflows(runClient, ctx)
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
