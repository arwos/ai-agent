/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package utils

import (
	"io"
)

const MaxResponseBodyBytes int64 = 100 << 20

// ReadAllResponse reads a response body with the service-wide safety limit.
func ReadAllResponse(body io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(body, MaxResponseBodyBytes))
}
