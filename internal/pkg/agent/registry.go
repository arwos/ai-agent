/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package agent

import (
	"sync"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/llm"
)

type Registry struct {
	pool          sync.Pool
	maxIterations int
}

func NewRegistry() *Registry {
	return NewRegistryWithLimit(8)
}
func NewRegistryWithLimit(maxIterations int) *Registry {
	if maxIterations <= 0 {
		maxIterations = 8
	}
	r := &Registry{maxIterations: maxIterations}
	r.pool.New = func() any { return &Engine{MaxIterations: maxIterations} }
	return r
}
func (r *Registry) Acquire(provider llm.Provider, tools ToolExecutor, store *dialog.Store, prompt string) *Engine {
	e := r.pool.Get().(*Engine)
	e.Provider, e.Tools, e.Store, e.SystemPrompt = provider, tools, store, prompt
	e.ToolProvider, e.ToolModel, e.ToolProviderName = nil, "", ""
	e.OnTool = nil
	return e
}
func (r *Registry) Release(e *Engine) {
	if e == nil {
		return
	}
	*e = Engine{MaxIterations: r.maxIterations}
	r.pool.Put(e)
}
