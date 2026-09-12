package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// Каталог в аргументе - сводка, файл - сами находки. Решает allFiles, потому
// что смешанный список "файл и каталог" всё равно даёт простыню.
func TestAllFilesPicksDetailMode(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{file}, true},
		{[]string{dir}, false},
		{[]string{file, dir}, false},
		{[]string{filepath.Join(dir, "нет.go")}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := allFiles(c.args); got != c.want {
			t.Errorf("allFiles(%v) = %v, ожидалось %v", c.args, got, c.want)
		}
	}
}
