/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package skills

import (
	"fmt"

	"go.osspkg.com/goppy/v3/plugin"
)

type ConfigGroup struct {
	Skills Config `yaml:"skills"`
}
type Config struct {
	Folder string `yaml:"folder"`
}

func (c *ConfigGroup) Default() {
	if c.Skills.Folder == "" {
		c.Skills.Folder = "./datasource/skills"
	}
}
func (c Config) Validate() error {
	if c.Folder == "" {
		return fmt.Errorf("skills folder is required")
	}
	return nil
}
func WithPlugin() plugin.Kind {
	return plugin.Kind{Config: &ConfigGroup{}, Inject: func(c *ConfigGroup) *Service { return New(c.Skills.Folder) }}
}
