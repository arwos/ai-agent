package updater

import "testing"

func TestNewer(t *testing.T) {
	for _, test := range []struct {
		candidate, current string
		want               bool
	}{
		{"v1.2.0", "v1.1.0", true}, {"v1.10.0", "v1.2.0", true}, {"v1.1.0", "v1.1.0", false}, {"v1.1.0", "v1.2.0", false}, {"v1.0.0", "v0.0.0-dev", true},
	} {
		if got := newer(test.candidate, test.current); got != test.want {
			t.Fatalf("newer(%q, %q) = %v", test.candidate, test.current, got)
		}
	}
}
