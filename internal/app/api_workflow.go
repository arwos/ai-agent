package app

import (
	"fmt"
	"github.com/arwos/ai-agent/internal/pkg/workflow"
	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"
)

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
