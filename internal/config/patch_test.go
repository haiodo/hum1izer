package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Комментарии в .hum1izer.yaml - это его документация, правка значения не должна
// их стирать.
func TestPatchKeepsComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	src := `# Настройки проекта.
version: 1

comments:
  # Комментарий длиннее скольких строк считать находкой.
  max_lines: 3
  commits: 20

languages:
  # only - белый список.
  only: []
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Patch(path, map[string]any{
		"comments.max_lines":  2,
		"comments.skip_tests": true,
		"languages.only":      []string{"go", "ts"},
		"baseline":            ".hum1izer-baseline",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		"# Настройки проекта.",
		"# Комментарий длиннее скольких строк считать находкой.",
		"# only - белый список.",
		"max_lines: 2",
		"skip_tests: true",
		"only: [go, ts]",
		"baseline: .hum1izer-baseline",
		"commits: 20",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("нет %q в:\n%s", want, text)
		}
	}
	if strings.Contains(text, "max_lines: 3") {
		t.Error("старое значение осталось")
	}

	// Файл должен остаться читаемым для строгого загрузчика.
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Comments.MaxLines == nil || *cfg.Comments.MaxLines != 2 {
		t.Errorf("max_lines не прочитался: %+v", cfg.Comments.MaxLines)
	}
	if len(cfg.Languages.Only) != 2 {
		t.Errorf("languages.only: %v", cfg.Languages.Only)
	}
}
