/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"context"
	"os"
	"time"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"
	"go.osspkg.com/logx"

	"github.com/arwos/ai-agent/internal/pkg/updater"
)

func (a *App) updateStatus(ev event.Event, _ ws.Meta) error {
	// A startup check is intentionally non-blocking. Refresh here so opening the
	// version dialog shortly after startup still has current release data.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	release, err := a.updater.Check(ctx)
	if err != nil {
		return err
	}
	return ev.Encode(release)
}

func (a *App) updateApply(ev event.Event, _ ws.Meta) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	script, err := a.updater.Prepare(ctx)
	if err != nil {
		return err
	}
	if err := ev.Encode(map[string]bool{"started": true}); err != nil {
		return err
	}
	go func() {
		time.Sleep(500 * time.Millisecond) // let the WebSocket response reach the browser
		if err := updater.StartScript(script); err != nil {
			logx.Error("failed to start update script", "err", err)
			return
		}
		logx.Info("update script started", "script", script)
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}
