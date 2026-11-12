package version

import "testing"

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.9", true},
		{"0.1.10", "0.1.9", true},
		{"0.1.0", "0.1.0-dev", true},
		{"0.1.0-dev", "0.1.0", false},
		{"0.1.0", "0.1.0", false},
		{"v1.0.0", "0.9.9", true},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}
