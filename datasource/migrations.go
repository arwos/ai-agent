/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package datasource

import (
	"embed"
	"io/fs"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/dialect"
)

//go:embed migration/*.sql
var migrationFS embed.FS

// Migrations returns the schema bundled into the service binary. Keeping the
// filenames unchanged preserves goppy's applied-migration ordering.
func Migrations() orm.Migration {
	data := make(map[string]string)
	entries, _ := fs.ReadDir(migrationFS, "migration")
	for _, entry := range entries {
		content, err := fs.ReadFile(migrationFS, "migration/"+entry.Name())
		if err == nil {
			data[entry.Name()] = string(content)
		}
	}
	return orm.Migration{Tags: []string{"master"}, Dialect: dialect.Name("sqlite"), Data: data}
}
