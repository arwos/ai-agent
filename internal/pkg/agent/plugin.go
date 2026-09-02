/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package agent

import "go.osspkg.com/goppy/v3/plugin"

type ConfigGroup struct {
	Agent Config `yaml:"agent"`
}
type Config struct {
	MaxIterations int `yaml:"max_iterations"`
}

func (c *ConfigGroup) Default() {
	if c.Agent.MaxIterations <= 0 {
		c.Agent.MaxIterations = 8
	}
}
func WithPlugin() plugin.Kind {
	return plugin.Kind{Config: &ConfigGroup{}, Inject: func(c *ConfigGroup) *Registry { return NewRegistryWithLimit(c.Agent.MaxIterations) }}
}
