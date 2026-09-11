package cli

import "testing"

func TestFenceFor(t *testing.T) {
	cases := []struct{ text, want string }{
		{"обычный текст", "```"},
		{"код в строке `x` внутри", "```"},
		{"блок\n```go\nx := 1\n```\n", "````"},
		{"````четыре````", "`````"},
	}
	for _, c := range cases {
		if got := fenceFor(c.text); got != c.want {
			t.Errorf("%q: забор %q, ожидался %q", c.text, got, c.want)
		}
	}
}
