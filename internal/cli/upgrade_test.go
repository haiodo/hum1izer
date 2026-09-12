package cli

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.4.0", "1.3.0", true},
		{"1.3.1", "1.3.0", true},
		{"2.0.0", "1.9.9", true},
		{"1.3.0", "1.3.0", false},
		{"1.2.0", "1.3.0", false},
		{"1.3", "1.3.0", false},
		{"1.4.0-rc1", "1.3.0", true},
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v, хотели %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSumFor(t *testing.T) {
	sums := []byte("aaa  hum1izer_1.3.0_linux_amd64.tar.gz\nbbb *hum1izer_1.3.0_darwin_arm64.tar.gz\n")
	if got := sumFor(sums, "hum1izer_1.3.0_darwin_arm64.tar.gz"); got != "bbb" {
		t.Errorf("sumFor = %q, хотели bbb", got)
	}
	if got := sumFor(sums, "нет такого"); got != "" {
		t.Errorf("sumFor на чужом имени = %q, хотели пусто", got)
	}
}
