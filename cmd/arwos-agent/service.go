//go:build !linux && !darwin && !windows

/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package main

func installAsService() {
	fatalService("service installation is not supported on this operating system")
}
