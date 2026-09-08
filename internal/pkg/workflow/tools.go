package workflow

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/arwos/ai-agent/internal/pkg/configstore"
	"github.com/arwos/ai-agent/internal/pkg/skills"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

// RegisterTools exposes profile-scoped workflow management as a builtin MCP.
func RegisterTools(reg *toolexecutor.Registry, service *Service, store *configstore.Store, skillService *skills.Service) error {
	read := func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
		id, _ := args["id"].(string)
		if id == "" {
			return service.List(scope.ProfileID)
		}
		return service.Get(scope.ProfileID, id)
	}
	validate := func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
		d, err := loadDefinition(service, scope.ProfileID, args)
		if err != nil {
			return nil, err
		}
		if err = Validate(d); err != nil {
			return map[string]any{"valid": false, "error": err.Error()}, nil
		}
		if err = validateSkillGroups(scope.ProfileID, d, skillService); err != nil {
			return map[string]any{"valid": false, "error": err.Error()}, nil
		}
		return map[string]any{"valid": true, "version": d.Version}, nil
	}
	write := func(operation string, mutate func(*Definition, map[string]any) error) toolexecutor.Tool {
		return toolexecutor.Tool{Name: "workflow." + operation, Description: "Modify a workflow with validation and user approval.", InputSchema: workflowSchema(operation), Handler: func(ctx context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			if scope.Mode == "consult" || scope.Mode == "workflow" {
				return nil, fmt.Errorf("workflow writes are unavailable in %s mode", scope.Mode)
			}
			id, _ := args["id"].(string)
			if id == "" {
				return nil, fmt.Errorf("workflow id is required")
			}
			before, err := service.Get(scope.ProfileID, id)
			if err != nil {
				return nil, err
			}
			if expected, ok := number(args["expected_version"]); ok && int(expected) != before.Version {
				return nil, fmt.Errorf("workflow version conflict: expected %d, current %d", int(expected), before.Version)
			}
			after := before
			if err = mutate(&after, args); err != nil {
				return nil, err
			}
			if err = Validate(after); err != nil {
				return nil, err
			}
			if err = validateSkillGroups(scope.ProfileID, after, skillService); err != nil {
				return nil, err
			}
			if after.Enabled {
				if err = ValidateResources(scope.ProfileID, after, reg, store); err != nil {
					return nil, err
				}
			}
			approved, err := approve(scope, ctx, operation, before, after)
			if err != nil || !approved {
				return nil, fmt.Errorf("workflow change was not approved")
			}
			saved, err := service.Save(scope.ProfileID, after)
			if err != nil {
				return nil, err
			}
			return map[string]any{"workflow": saved, "diff": diff(before, saved)}, nil
		}}
	}
	create := toolexecutor.Tool{Name: "workflow.create", Description: "Create a disabled workflow draft. Add valid steps with workflow.step_add, then enable it with workflow.update.", InputSchema: createWorkflowSchema(), Handler: func(ctx context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
		if scope.Mode == "consult" || scope.Mode == "workflow" {
			return nil, fmt.Errorf("workflow writes are unavailable in %s mode", scope.Mode)
		}
		name, _ := args["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("workflow name is required")
		}
		if items, err := service.List(scope.ProfileID); err != nil {
			return nil, err
		} else {
			for _, item := range items {
				if strings.EqualFold(item.Name, name) {
					return map[string]any{"workflow": item, "created": false, "next": workflowNext(item)}, nil
				}
			}
		}
		d := Definition{Version: 0, ID: newID(), Name: name, Enabled: false}
		if description, ok := args["description"].(string); ok {
			d.Description = description
		}
		if err := Validate(d); err != nil {
			return nil, err
		}
		if err := validateSkillGroups(scope.ProfileID, d, skillService); err != nil {
			return nil, err
		}
		approved, err := approve(scope, ctx, "create", Definition{}, d)
		if err != nil || !approved {
			return nil, fmt.Errorf("workflow creation was not approved")
		}
		saved, err := service.Save(scope.ProfileID, d)
		if err != nil {
			return nil, err
		}
		return map[string]any{"workflow": saved, "created": true, "diff": diff(Definition{}, saved), "next": workflowNext(saved)}, nil
	}}
	agentCatalog := toolexecutor.Tool{Name: "workflow.agent_catalog", Description: "List profile agents that can execute workflow steps.", Handler: func(_ context.Context, scope toolexecutor.Scope, _ map[string]any) (any, error) {
		return store.Agents(scope.ProfileID)
	}}
	resourceCatalog := toolexecutor.Tool{Name: "workflow.resource_catalog", Description: "List available MCP tools and skill groups for the active profile.", Handler: func(_ context.Context, scope toolexecutor.Scope, _ map[string]any) (any, error) {
		groups, err := skillService.Groups(scope.ProfileID)
		if err != nil {
			return nil, err
		}
		catalog, err := Catalog(scope.ProfileID, reg, store)
		if err != nil {
			return nil, err
		}
		return map[string]any{"skill_groups": groups, "mcp": catalog}, nil
	}}
	deleteTool := toolexecutor.Tool{Name: "workflow.delete", Description: "Delete a workflow after user approval.", InputSchema: workflowSchema("delete"), Handler: func(ctx context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
		if scope.Mode == "consult" || scope.Mode == "workflow" {
			return nil, fmt.Errorf("workflow writes are unavailable in %s mode", scope.Mode)
		}
		id, _ := args["id"].(string)
		if id == "" {
			return nil, fmt.Errorf("workflow id is required")
		}
		before, err := service.Get(scope.ProfileID, id)
		if err != nil {
			return nil, err
		}
		if expected, ok := number(args["expected_version"]); ok && int(expected) != before.Version {
			return nil, fmt.Errorf("workflow version conflict: expected %d, current %d", int(expected), before.Version)
		}
		approved, err := approve(scope, ctx, "delete", before, Definition{ID: id})
		if err != nil || !approved {
			return nil, fmt.Errorf("workflow deletion was not approved")
		}
		if err := service.Delete(scope.ProfileID, id); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true, "workflow_id": id, "version": before.Version}, nil
	}}
	for _, tool := range []toolexecutor.Tool{
		{Name: "workflow.list", Description: "List workflows in the active profile."},
		{Name: "workflow.get", Description: "Get a workflow in the active profile."},
		{Name: "workflow.validate", Description: "Validate a workflow without saving it."},
		create,
		deleteTool,
		write("update", func(d *Definition, a map[string]any) error {
			if v, ok := a["name"].(string); ok {
				d.Name = v
			}
			if v, ok := a["description"].(string); ok {
				d.Description = v
			}
			if v, ok := a["enabled"].(bool); ok {
				d.Enabled = v
			}
			return nil
		}),
		write("step_add", func(d *Definition, a map[string]any) error {
			raw, ok := a["step"].(map[string]any)
			if !ok {
				return fmt.Errorf("step is required")
			}
			var step Step
			if err := decodeStep(raw, &step); err != nil {
				return err
			}
			index := len(d.Steps)
			if n, ok := number(a["index"]); ok {
				index = int(n)
			}
			if index < 0 || index > len(d.Steps) {
				return fmt.Errorf("invalid step index")
			}
			d.Steps = append(d.Steps, Step{})
			copy(d.Steps[index+1:], d.Steps[index:])
			d.Steps[index] = step
			return nil
		}),
		write("step_update", func(d *Definition, a map[string]any) error {
			sid, _ := a["step_id"].(string)
			for i := range d.Steps {
				if d.Steps[i].ID == sid {
					return decodeStep(a, &d.Steps[i])
				}
			}
			return fmt.Errorf("step %q not found", sid)
		}),
		write("step_remove", func(d *Definition, a map[string]any) error {
			sid, _ := a["step_id"].(string)
			for _, s := range d.Steps {
				for _, dep := range s.Dependencies {
					if dep == sid {
						return fmt.Errorf("step %q is used as a dependency", sid)
					}
				}
			}
			for i := range d.Steps {
				if d.Steps[i].ID == sid {
					d.Steps = append(d.Steps[:i], d.Steps[i+1:]...)
					return nil
				}
			}
			return fmt.Errorf("step %q not found", sid)
		}),
		write("step_reorder", func(d *Definition, a map[string]any) error {
			sid, _ := a["step_id"].(string)
			n, ok := number(a["index"])
			if !ok {
				return fmt.Errorf("index is required")
			}
			from := -1
			for i := range d.Steps {
				if d.Steps[i].ID == sid {
					from = i
				}
			}
			if from < 0 || int(n) < 0 || int(n) >= len(d.Steps) {
				return fmt.Errorf("invalid step or index")
			}
			step := d.Steps[from]
			d.Steps = append(d.Steps[:from], d.Steps[from+1:]...)
			target := int(n)
			if target > len(d.Steps) {
				target = len(d.Steps)
			}
			d.Steps = append(d.Steps, Step{})
			copy(d.Steps[target+1:], d.Steps[target:])
			d.Steps[target] = step
			return nil
		}),
		agentCatalog,
		resourceCatalog,
	} {
		if tool.Name == "workflow.list" || tool.Name == "workflow.get" {
			tool.Handler = read
			tool.InputSchema = map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}}
		}
		if tool.Name == "workflow.validate" {
			tool.Handler = validate
			tool.InputSchema = workflowSchema("validate")
		}
		if err := reg.Register(tool); err != nil {
			return err
		}
	}
	return reg.RegisterBuiltin(toolexecutor.BuiltinServer{Key: "workflow", Name: "Workflow", Description: "Read and manage workflows in the active profile.", Prefix: "workflow", Tools: builtinWorkflowTools()})
}

func workflowSchema(operation string) map[string]any {
	switch operation {
	case "step_add":
		return stepAddSchema()
	case "step_update":
		return stepUpdateSchema()
	case "update":
		return map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "expected_version": map[string]any{"type": "integer"}, "name": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}, "enabled": map[string]any{"type": "boolean"}}, "required": []string{"id", "expected_version"}}
	default:
		return map[string]any{"type": "object", "additionalProperties": true}
	}
}

func createWorkflowSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":        map[string]any{"type": "string", "description": "Human-readable workflow name."},
			"description": map[string]any{"type": "string"},
		},
		"required": []string{"name"},
	}
}

func stepProperties() map[string]any {
	return map[string]any{
		"title":           map[string]any{"type": "string", "description": "Short step name."},
		"description":     map[string]any{"type": "string"},
		"acceptance":      map[string]any{"type": "string", "description": "Concrete criteria that the reviewer can verify."},
		"agent_id":        map[string]any{"type": "string", "description": "ID from workflow.agent_catalog."},
		"dependencies":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "IDs of preceding workflow steps only."},
		"required_tools":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Any full tool name listed for the selected agent in workflow.resource_catalog.agents[].tools."},
		"required_skills": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Skill group IDs from workflow.resource_catalog, not individual skill names."},
		"max_attempts":    map[string]any{"type": "integer", "minimum": 1, "maximum": 10},
	}
}

func stepAddSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string", "description": "Workflow ID returned by workflow.create."}, "expected_version": map[string]any{"type": "integer"}, "index": map[string]any{"type": "integer"}, "step": map[string]any{"type": "object", "properties": stepProperties(), "required": []string{"title", "acceptance", "agent_id", "max_attempts", "required_tools", "required_skills"}}}, "required": []string{"id", "expected_version", "step"}}
}

func stepUpdateSchema() map[string]any {
	properties := stepProperties()
	properties["id"] = map[string]any{"type": "string"}
	properties["expected_version"] = map[string]any{"type": "integer"}
	properties["step_id"] = map[string]any{"type": "string"}
	return map[string]any{"type": "object", "properties": properties, "required": []string{"id", "expected_version", "step_id"}}
}

func workflowNext(d Definition) map[string]any {
	return map[string]any{"workflow_id": d.ID, "expected_version": d.Version, "instruction": "Use workflow.step_add with this workflow_id and expected_version for every step. Each step requires title, acceptance, agent_id, max_attempts, required_tools, and required_skills. Always provide both arrays, even when empty; choose tools from the selected agent catalog and skill groups from workflow.resource_catalog. Keep enabled false until every step is valid; add user.ask to required_tools when the Builder must request input at execution time."}
}
func builtinWorkflowTools() []toolexecutor.BuiltinTool {
	names := []string{"list", "get", "validate", "create", "update", "delete", "step_add", "step_update", "step_remove", "step_reorder", "agent_catalog", "resource_catalog"}
	out := make([]toolexecutor.BuiltinTool, 0, len(names))
	for _, n := range names {
		schema := workflowSchema(n)
		if n == "create" {
			schema = createWorkflowSchema()
		}
		out = append(out, toolexecutor.BuiltinTool{ToolName: "workflow." + n, Alias: n, Description: "Workflow management operation: " + n, InputSchema: schema})
	}
	return out
}
func loadDefinition(s *Service, profile string, args map[string]any) (Definition, error) {
	if raw, ok := args["workflow"].(Definition); ok {
		return raw, nil
	}
	id, _ := args["id"].(string)
	if id == "" {
		return Definition{}, fmt.Errorf("workflow id is required")
	}
	return s.Get(profile, id)
}
func decodeStep(a map[string]any, s *Step) error {
	if v, ok := a["id"].(string); ok && v != "" {
		s.ID = v
	}
	if v, ok := a["title"].(string); ok {
		s.Title = v
	}
	if v, ok := a["description"].(string); ok {
		s.Description = v
	}
	if v, ok := a["acceptance"].(string); ok {
		s.Acceptance = v
	}
	if v, ok := a["agent_id"].(string); ok {
		s.AgentID = v
	}
	if v, ok := stringSlice(a["dependencies"]); ok {
		s.Dependencies = v
	}
	if v, ok := stringSlice(a["required_tools"]); ok {
		s.RequiredTools = v
	}
	if v, ok := stringSlice(a["required_skills"]); ok {
		s.RequiredSkills = v
	}
	if v, ok := number(a["max_attempts"]); ok {
		s.Retry.MaxAttempts = int(v)
	}
	if s.ID == "" {
		s.ID = newID()
	}
	return nil
}
func stringSlice(v any) ([]string, bool) {
	switch values := v.(type) {
	case []string:
		return append([]string(nil), values...), true
	case []any:
		out := make([]string, 0, len(values))
		for _, item := range values {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func validateSkillGroups(profile string, definition Definition, service *skills.Service) error {
	groups, err := service.Groups(profile)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(groups))
	for _, group := range groups {
		known[group.ID] = true
	}
	for _, step := range definition.Steps {
		for _, group := range step.RequiredSkills {
			if !known[group] {
				return fmt.Errorf("workflow step %q requires unknown skill group %q; use an ID from workflow.resource_catalog", step.ID, group)
			}
		}
	}
	return nil
}
func number(v any) (float64, bool) { n, ok := v.(float64); return n, ok }

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("workflow-%d", len(b))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func approve(scope toolexecutor.Scope, ctx context.Context, operation string, before, after Definition) (bool, error) {
	if scope.Request == nil {
		return false, fmt.Errorf("approval is unavailable")
	}
	v, err := scope.Request(ctx, map[string]any{"kind": "approval", "title": "Workflow change requires approval", "detail": operation, "workflow": map[string]any{"before": before, "after": after, "diff": diff(before, after)}})
	if err != nil {
		return false, err
	}
	ok, valid := v.(bool)
	return ok && valid, nil
}
func diff(before, after Definition) map[string]any {
	toVersion := after.Version
	if toVersion <= before.Version {
		toVersion = before.Version + 1
	}
	return map[string]any{"operation": "update", "workflow_id": after.ID, "from_version": before.Version, "to_version": toVersion, "before_steps": len(before.Steps), "after_steps": len(after.Steps)}
}
