package workflow

import (
	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"go.osspkg.com/goppy/v3/plugin"
	"path/filepath"
)

func WithPlugin() plugin.Kind {
	return plugin.Kind{Inject: func(d *dialog.Store) *Service { return New(filepath.Dir(d.Root)) }}
}
