/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

// Package frontend exposes the production React/Vite bundle embedded in the Go binary.
package frontend

import (
	"embed"
	"io/fs"
)

// dist contains the output of `pnpm --dir web build`.
//
//go:embed dist
var dist embed.FS

// Browser is the React/Vite bundle with dist as its root.
var Browser = mustSub(dist, "dist")

func mustSub(fsys fs.FS, dir string) fs.FS {
	result, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return result
}
