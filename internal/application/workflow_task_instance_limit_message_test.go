package application

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ScienJus/kairos/internal/domain"
)

func TestWorkflowTaskInstanceLimitMessageBoundary(t *testing.T) {
	const source = domain.TaskID("source")
	const limit = 1
	format := func(node string) string {
		return fmt.Sprintf("workflow node %q reached max_task_instances_per_node (1); cannot create task instance 2 after task source", node)
	}
	budget := domain.MaxHistoryTextBytes - len(format(""))
	for _, node := range []string{
		strings.Repeat("x", budget),
		strings.Repeat("x", budget+1),
		strings.Repeat("界", budget/3) + strings.Repeat("x", budget%3),
		strings.Repeat("界", budget/3) + strings.Repeat("x", budget%3+1),
		strings.Repeat("\n", budget/2+1), // Quoting expands the stored ID.
	} {
		message := workflowTaskInstanceLimitMessage(domain.WorkflowTaskID(node), source, limit)
		if len(message) > domain.MaxHistoryTextBytes || !utf8.ValidString(message) {
			t.Fatal("generated failure message exceeds its UTF-8 byte budget")
		}
		if !strings.Contains(message, "reached max_task_instances_per_node (1); cannot create task instance 2") {
			t.Fatal("message lost the guard-specific cause")
		}
		if len(format(node)) <= domain.MaxHistoryTextBytes {
			if message != format(node) {
				t.Fatal("message at the boundary must retain its full detail")
			}
		} else if !strings.Contains(message, "failure.workflow_task_id") {
			t.Fatal("abbreviated message must point to the complete node ID")
		}
	}
}
