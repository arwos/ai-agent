/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package vectordb

import (
	"fmt"

	"go.osspkg.com/goppy/v3/plugin"
)

type ConfigGroup struct {
	Vectordb Config `yaml:"vectordb"`
}
type Config struct {
	Folder  string `yaml:"folder"`
	Address string `yaml:"addr"`
}

func (c *ConfigGroup) Default() {
	if c.Vectordb.Folder == "" {
		c.Vectordb.Folder = "./datasource/vectordb"
	}
	if c.Vectordb.Address == "" {
		c.Vectordb.Address = "127.0.0.1:6334"
	}
}
func (c Config) Validate() error {
	if c.Folder == "" {
		return fmt.Errorf("vectordb folder is required")
	}
	if c.Address == "" {
		return fmt.Errorf("vectordb address is required")
	}
	return nil
}

func WithPlugin() plugin.Kind {
	return plugin.Kind{Config: &ConfigGroup{}, Inject: func(c *ConfigGroup) *Service {
		return New(c.Vectordb.Folder, c.Vectordb.Address)
	}}
}
