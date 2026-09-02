/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package pkg

import (
	"go.osspkg.com/goppy/v3/plugin"

	"github.com/arwos/ai-agent/internal/pkg/agent"
	"github.com/arwos/ai-agent/internal/pkg/configstore"
	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/gittools"
	"github.com/arwos/ai-agent/internal/pkg/jobs"
	"github.com/arwos/ai-agent/internal/pkg/knowledge"
	"github.com/arwos/ai-agent/internal/pkg/llama"
	"github.com/arwos/ai-agent/internal/pkg/mcp"
	"github.com/arwos/ai-agent/internal/pkg/ollama"
	"github.com/arwos/ai-agent/internal/pkg/proxy"
	"github.com/arwos/ai-agent/internal/pkg/skills"
	"github.com/arwos/ai-agent/internal/pkg/systeminfo"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
	"github.com/arwos/ai-agent/internal/pkg/updater"
	"github.com/arwos/ai-agent/internal/pkg/workspace"
)

var Plugins = plugin.Inject(
	configstore.New,
	jobs.WithPlugin(),
	updater.New,
	workspace.New,
	toolexecutor.WithPlugin(),
	workspace.NewToolRegistration,
	gittools.NewToolRegistration,
	workspace.NewPicker,
	skills.WithPlugin(),
	skills.RegisterTools,
	dialog.WithPlugin(),
	dialog.NewRegistry,
	mcp.New,
	proxy.New,
	knowledge.WithPlugin(),
	func(info *systeminfo.Service) *llama.Service { return llama.New(info.Root()) },
	func(info *systeminfo.Service) *ollama.Service { return ollama.New(info.Root()) },
	systeminfo.WithPlugin(),
	agent.WithPlugin(),
)
