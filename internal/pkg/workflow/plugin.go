package workflow

import (
	"github.com/arwos/ai-agent/internal/pkg/configstore"
	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/skills"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
	"go.osspkg.com/goppy/v3/plugin"
	"path/filepath"
)

func WithPlugin() plugin.Kind {
	return plugin.Kind{Inject: func(d *dialog.Store) *Service { return New(filepath.Dir(d.Root)) }}
}

// ToolRegistration is intentionally named so the DI container can distinguish
// workflow registration from other builtin MCP registrations.
type ToolRegistration struct{}

func NewToolRegistration(reg *toolexecutor.Registry, service *Service, store *configstore.Store, skillService *skills.Service) (ToolRegistration, error) {
	return ToolRegistration{}, RegisterTools(reg, service, store, skillService)
}
