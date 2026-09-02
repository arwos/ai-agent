/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package dialog

import (
	"fmt"

	"go.osspkg.com/goppy/v3/plugin"
)

type ConfigGroup struct {
	Dialogs Config `yaml:"dialogs"`
}
type Config struct {
	Folder       string `yaml:"folder"`
	HistoryLimit int    `yaml:"history_limit"`
}

func (c *ConfigGroup) Default() {
	if c.Dialogs.Folder == "" {
		c.Dialogs.Folder = "./datasource/dialogs"
	}
	if c.Dialogs.HistoryLimit <= 0 {
		c.Dialogs.HistoryLimit = 100
	}
}
func (c Config) Validate() error {
	if c.Folder == "" {
		return fmt.Errorf("dialogs folder is required")
	}
	return nil
}
func WithPlugin() plugin.Kind {
	return plugin.Kind{Config: &ConfigGroup{}, Inject: func(c *ConfigGroup) *Store { return NewStore(c.Dialogs.Folder, c.Dialogs.HistoryLimit) }}
}
