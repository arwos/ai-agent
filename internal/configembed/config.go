/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package configembed

import _ "embed"

// Default is the development configuration shipped with the binary.
//
//go:embed config.yaml
var Default []byte
