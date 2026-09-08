/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import "testing"

func TestValidateExternalURLRejectsLocalTargets(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8080/secret",
		"http://[::1]/secret",
		"http://10.0.0.1/secret",
		"http://169.254.169.254/latest/meta-data/",
		"file:///etc/passwd",
		"http://user:pass@example.com/",
	} {
		if err := validateExternalURL(raw); err == nil {
			t.Fatalf("validateExternalURL(%q) unexpectedly succeeded", raw)
		}
	}
	if err := validateExternalURL("https://example.com/skill.md"); err != nil {
		t.Fatalf("public URL rejected: %v", err)
	}
}
