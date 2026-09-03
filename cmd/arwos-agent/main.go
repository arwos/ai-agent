/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package main

import (
	"context"

	"go.osspkg.com/goppy/v3"
	"go.osspkg.com/goppy/v3/pkg/console"
	"go.osspkg.com/goppy/v3/plugin"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/goppy/v3/plugins/ws"

	"github.com/arwos/ai-agent/datasource"
	"github.com/arwos/ai-agent/internal/app"
	"github.com/arwos/ai-agent/internal/pkg"
	"github.com/arwos/ai-agent/internal/pkg/version"
)

var Version = version.Value

func main() {
	// Specify the path to the config via the argument: `--config`.
	// Specify the path to the pidfile via the argument: `--pid`.
	svc := goppy.New("arwos-agent", Version, "Arwos AI Agent")
	svc.Command(func(_ context.Context, _ plugin.DIResolver, setter console.CommandSetter) {
		setter.Setup("install-as-service", "Install a systemd user service")
		setter.ExecFunc(installAsService)
	})
	svc.Plugins(
		web.WithServer(),
		ws.WithServer(),
		orm.WithORM(sqlite.Name),
		orm.WithMigration(datasource.Migrations()),
	)
	svc.Plugins(
		pkg.Plugins,
		app.Plugins,
	)
	svc.Run()
}
