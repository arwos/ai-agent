package app

import (
	"fmt"
	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/goal"
	"github.com/arwos/ai-agent/internal/pkg/workflow"
	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"
)

func (a *App) workflowGoal(ev event.Event) (string, *goal.Goal, error) {
	var q struct {
		DialogID string `json:"dialog_id"`
	}
	if err := ev.Decode(&q); err != nil {
		return "", nil, err
	}
	if q.DialogID == "" {
		return "", nil, fmt.Errorf("dialog_id is required")
	}
	profileID, err := a.workflowProfile()
	if err != nil {
		return "", nil, err
	}
	g, err := a.dialogs.Goal(dialog.SessionKey(profileID, q.DialogID))
	if err != nil {
		return "", nil, err
	}
	if g == nil || g.WorkflowID == "" {
		return "", nil, fmt.Errorf("active workflow not found")
	}
	return profileID, g, nil
}

func (a *App) workflowPause(ev event.Event, _ ws.Meta) error {
	profileID, g, err := a.workflowGoal(ev)
	if err != nil {
		return err
	}
	if g.Status != "running" {
		return ev.Encode(map[string]any{"status": g.Status})
	}
	g.Status, g.CurrentTaskID = "paused", ""
	for index := range g.Tasks {
		if g.Tasks[index].Status == "waiting_agent" || g.Tasks[index].Status == "reviewing" || g.Tasks[index].Status == "running" {
			g.Tasks[index].Status = "pending"
		}
	}
	a.cancelWorkflowChildren(profileID, g.DialogID)
	if err = a.dialogs.SaveGoal(dialog.SessionKey(profileID, g.DialogID), *g); err != nil {
		return err
	}
	a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": g.DialogID, "goal": g}})
	return ev.Encode(map[string]any{"status": g.Status})
}

func (a *App) workflowContinue(ev event.Event, meta ws.Meta) error {
	profileID, g, err := a.workflowGoal(ev)
	if err != nil {
		return err
	}
	if g.Status != "paused" {
		return ev.Encode(map[string]any{"status": g.Status})
	}
	conversation, err := a.store.Conversation(profileID, g.DialogID)
	if err != nil {
		return err
	}
	g.Status = "running"
	if err = a.dialogs.SaveGoal(dialog.SessionKey(profileID, g.DialogID), *g); err != nil {
		return err
	}
	a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": g.DialogID, "goal": g}})
	if err = a.scheduleWorkflow(meta.Context(), profileID, g.DialogID, conversation.WorkspaceID, g); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"status": g.Status})
}

func (a *App) workflowStop(ev event.Event, _ ws.Meta) error {
	profileID, g, err := a.workflowGoal(ev)
	if err != nil {
		return err
	}
	a.cancelChildJobs(profileID, g.DialogID)
	g.Status = "stopped"
	goal.CancelWorkflowTasks(g)
	if err = a.dialogs.SaveGoal(dialog.SessionKey(profileID, g.DialogID), *g); err != nil {
		return err
	}
	a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": g.DialogID, "goal": g}})
	return ev.Encode(map[string]any{"status": g.Status})
}

func (a *App) cancelWorkflowChildren(profileID, parentID string) {
	a.childMu.RLock()
	runs := make([]childRun, 0)
	for _, run := range a.childRuns {
		if run.ProfileID == profileID && run.ParentDialogID == parentID {
			runs = append(runs, run)
		}
	}
	a.childMu.RUnlock()
	for _, run := range runs {
		a.chatMu.RLock()
		job := a.chatJobs[run.ChildDialogID]
		a.chatMu.RUnlock()
		if job != nil {
			job.cancel()
		}
		a.childMu.Lock()
		delete(a.childRuns, run.ChildDialogID)
		a.childMu.Unlock()
	}
}

func (a *App) workflowProfile() (string, error) { return a.activeProfile() }
func (a *App) workflowList(ev event.Event, _ ws.Meta) error {
	p, err := a.workflowProfile()
	if err != nil {
		return err
	}
	v, err := a.workflows.List(p)
	if err != nil {
		return err
	}
	return ev.Encode(v)
}
func (a *App) workflowGet(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("id is required")
	}
	p, err := a.workflowProfile()
	if err != nil {
		return err
	}
	v, err := a.workflows.Get(p, q.ID)
	if err != nil {
		return err
	}
	return ev.Encode(v)
}
func (a *App) workflowSave(ev event.Event, _ ws.Meta) error {
	var q workflow.Definition
	if err := ev.Decode(&q); err != nil {
		return err
	}
	p, err := a.workflowProfile()
	if err != nil {
		return err
	}
	if q.Enabled {
		if err = workflow.ValidateResources(p, q, a.tools, a.store); err != nil {
			return err
		}
	}
	v, err := a.workflows.Save(p, q)
	if err != nil {
		return err
	}
	return ev.Encode(v)
}
func (a *App) workflowDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	p, err := a.workflowProfile()
	if err != nil {
		return err
	}
	if err = a.workflows.Delete(p, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) workflowImport(ev event.Event, _ ws.Meta) error {
	var q struct {
		Content string `json:"content"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	p, err := a.workflowProfile()
	if err != nil {
		return err
	}
	v, err := a.workflows.Import(p, q.Content)
	if err != nil {
		return err
	}
	return ev.Encode(v)
}
func (a *App) workflowExport(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	p, err := a.workflowProfile()
	if err != nil {
		return err
	}
	v, err := a.workflows.Export(p, q.ID)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]string{"content": v})
}
