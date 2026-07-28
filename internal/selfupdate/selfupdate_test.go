package selfupdate

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.4", "1.0.4", 0},
		{"1.0.10", "1.0.4", 1}, // numeric, not lexicographic
		{"1.0.4", "1.0.10", -1},
		{"v1.0.5", "1.0.4", 1},
		{"1.0.4", "1.0.4.1", -1},
		{"1.1", "1.0.99", 1},
		{"2.0.0-rc1", "1.9.9", 1}, // suffix stripped
	}
	for _, c := range cases {
		got, err := CompareVersions(c.a, c.b)
		if err != nil {
			t.Errorf("CompareVersions(%q, %q): %v", c.a, c.b, err)
			continue
		}
		if got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if _, err := CompareVersions("1.x.0", "1.0.0"); err == nil {
		t.Error("non-numeric component must be an error")
	}
}
