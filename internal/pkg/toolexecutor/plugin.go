/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package toolexecutor

import "go.osspkg.com/goppy/v3/plugin"

func WithPlugin() plugin.Kind { return plugin.Kind{Inject: New} }
