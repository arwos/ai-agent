/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package knowledge

import (
	"fmt"

	"go.osspkg.com/goppy/v3/plugin"
)

type ConfigGroup struct {
	Knowledge Config `yaml:"knowledge"`
}
type Config struct {
	Folder      string `yaml:"folder"`
	PageSize    int    `yaml:"page_size"`
	SearchLimit int    `yaml:"search_limit"`
}

func (c *ConfigGroup) Default() {
	if c.Knowledge.Folder == "" {
		c.Knowledge.Folder = "./datasource/knowledge"
	}
	if c.Knowledge.PageSize <= 0 {
		c.Knowledge.PageSize = 10
	}
	if c.Knowledge.SearchLimit <= 0 {
		c.Knowledge.SearchLimit = 4
	}
}
func (c Config) Validate() error {
	if c.Folder == "" {
		return fmt.Errorf("knowledge folder is required")
	}
	return nil
}
func WithPlugin() plugin.Kind {
	return plugin.Kind{Config: &ConfigGroup{}, Inject: func(c *ConfigGroup) *Store {
		return New(c.Knowledge.Folder, c.Knowledge.PageSize, c.Knowledge.SearchLimit)
	}}
}
