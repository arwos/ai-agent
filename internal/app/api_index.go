/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"go.osspkg.com/goppy/v3/plugins/web"

	frontend "github.com/arwos/ai-agent/web"
)

func (a *App) ApiIndex(wc web.Ctx) {
	requested := strings.TrimPrefix(path.Clean(wc.URL().Path), "/")
	if requested == "." {
		requested = ""
	}

	if requested == "api" || strings.HasPrefix(requested, "api/") {
		wc.Error(http.StatusNotFound, errors.New("api endpoint not found"))
		return
	}

	if requested != "" {
		if content, err := fs.ReadFile(frontend.Browser, requested); err == nil {
			a.serveFrontendFile(wc, requested, content)
			return
		}
		if strings.Contains(path.Base(requested), ".") {
			wc.Error(http.StatusNotFound, errors.New("static asset not found"))
			return
		}
	}

	content, err := fs.ReadFile(frontend.Browser, "index.html")
	if err != nil {
		wc.Error(http.StatusInternalServerError, err)
		return
	}
	a.serveFrontendFile(wc, "index.html", content)
}

func (a *App) serveFrontendFile(wc web.Ctx, name string, content []byte) {
	wc.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	wc.Header().Set("Pragma", "no-cache")
	wc.Header().Set("Expires", "0")
	http.ServeContent(wc.Response(), wc.Request(), name, time.Time{}, bytes.NewReader(content))
}
