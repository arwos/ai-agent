/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package goal

import (
	"fmt"
	"strings"
	"time"
)

// StartWorkflowTask validates and starts exactly the next eligible workflow task.
func StartWorkflowTask(g *Goal, taskID string, capabilities map[string]bool) error {
	return StartWorkflowTaskWithPolicy(g, taskID, capabilities, "", "")
}

// StartWorkflowTaskWithPolicy applies the immutable task executor contract at runtime.
func StartWorkflowTaskWithPolicy(g *Goal, taskID string, capabilities map[string]bool, executor, agentID string) error {
	if g == nil || g.WorkflowID == "" {
		return fmt.Errorf("workflow goal is required")
	}
	var selected *GoalTask
	for i := range g.Tasks {
		task := &g.Tasks[i]
		if task.ID == taskID {
			selected = task
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("workflow task %q not found", taskID)
	}
	if executor != "" && selected.Executor != "" && selected.Executor != "auto" && selected.Executor != executor {
		return fmt.Errorf("workflow task %q requires executor %q", taskID, selected.Executor)
	}
	if selected.Executor == "agent_id" && selected.ExecutorAgentID != "" && selected.ExecutorAgentID != agentID {
		return fmt.Errorf("workflow task %q requires agent %q", taskID, selected.ExecutorAgentID)
	}
	if selected.Executor == "self" && executor == "agent_id" {
		return fmt.Errorf("workflow task %q cannot be delegated", taskID)
	}
	if selected.Status != "pending" {
		return fmt.Errorf("workflow task %q is not pending", taskID)
	}
	if g.CurrentTaskID != "" && g.CurrentTaskID != taskID {
		return fmt.Errorf("workflow task %q is not current", taskID)
	}
	for _, task := range g.Tasks {
		if task.ID == taskID {
			break
		}
		if task.Status != "done" && task.Status != "skipped" {
			return fmt.Errorf("workflow task %q is out of order", taskID)
		}
	}
	for _, other := range g.Tasks {
		if other.Status == "running" || other.Status == "waiting_agent" || other.Status == "reviewing" {
			return fmt.Errorf("workflow task %q is already active", other.ID)
		}
	}
	for _, dependency := range selected.DependsOn {
		found := false
		for _, other := range g.Tasks {
			if other.WorkflowStepID == dependency || other.ID == dependency {
				found = true
				if other.Status != "done" {
					return fmt.Errorf("workflow task %q depends on unfinished task %q", taskID, dependency)
				}
				break
			}
		}
		if !found {
			return fmt.Errorf("workflow task %q depends on unknown task %q", taskID, dependency)
		}
	}
	for _, capability := range selected.Tools {
		if !capabilities[capability] {
			return fmt.Errorf("workflow task %q requires unavailable capability %q", taskID, capability)
		}
	}
	for _, capability := range selected.Skills {
		if !capabilities[capability] {
			return fmt.Errorf("workflow task %q requires unavailable capability %q", taskID, capability)
		}
	}
	if selected.MaxAttempts < 1 {
		return fmt.Errorf("workflow task %q has invalid retry limit", taskID)
	}
	if selected.Attempts >= selected.MaxAttempts {
		return fmt.Errorf("workflow task %q exceeded retry limit", taskID)
	}
	selected.Status = "running"
	selected.Attempts++
	g.CurrentTaskID = taskID
	return nil
}

// ValidateWorkflowTaskActive verifies that delegation belongs to the current task.
func ValidateWorkflowTaskActive(g *Goal, taskID string) error {
	if g == nil || g.WorkflowID == "" {
		return fmt.Errorf("workflow goal is required")
	}
	for _, task := range g.Tasks {
		if task.ID != taskID {
			continue
		}
		if task.Status != "running" || (g.CurrentTaskID != "" && g.CurrentTaskID != taskID) {
			return fmt.Errorf("workflow task %q is not active", taskID)
		}
		for _, other := range g.Tasks {
			if other.ID != taskID && (other.Status == "running" || other.Status == "waiting_agent" || other.Status == "reviewing") {
				return fmt.Errorf("workflow task %q is not the current task", taskID)
			}
		}
		if task.MaxAttempts < 1 || task.Attempts > task.MaxAttempts {
			return fmt.Errorf("workflow task %q exceeded retry limit", taskID)
		}
		return nil
	}
	return fmt.Errorf("workflow task %q not found", taskID)
}

// CompleteWorkflowTask records one terminal task result and rejects stale callbacks.
func CompleteWorkflowTask(g *Goal, taskID, childDialogID, result string, taskErr error) error {
	if g == nil || g.WorkflowID == "" {
		return fmt.Errorf("workflow goal is required")
	}
	for i := range g.Tasks {
		task := &g.Tasks[i]
		if task.ID != taskID {
			continue
		}
		if task.Status != "running" && task.Status != "waiting_agent" && task.Status != "reviewing" {
			return fmt.Errorf("workflow task %q is not active", taskID)
		}
		if childDialogID != "" && task.ChildDialogID != childDialogID {
			return fmt.Errorf("workflow task %q child does not match", taskID)
		}
		if taskErr != nil {
			task.Error = strings.TrimSpace(taskErr.Error())
			if task.Attempts < task.MaxAttempts {
				task.Status = "pending"
			} else {
				task.Status = "failed"
			}
		} else {
			task.LastResult = result
			task.Status = "done"
		}
		if g.CurrentTaskID == taskID {
			g.CurrentTaskID = ""
		}
		return nil
	}
	return fmt.Errorf("workflow task %q not found", taskID)
}

// CancelWorkflowTasks marks active workflow work as stopped and clears the cursor.
func CancelWorkflowTasks(g *Goal) bool {
	if g == nil {
		return false
	}
	changed := false
	for index := range g.Tasks {
		if g.Tasks[index].Status != "running" && g.Tasks[index].Status != "waiting_agent" && g.Tasks[index].Status != "reviewing" {
			continue
		}
		g.Tasks[index].Status = "stopped"
		g.Tasks[index].FinishedAt = time.Now().UTC()
		changed = true
	}
	if changed {
		g.CurrentTaskID = ""
	}
	return changed
}
