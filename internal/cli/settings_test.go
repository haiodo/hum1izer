package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/haiodo/hum1izer/internal/config"
)

func settingsOn(t *testing.T, body string) (settings, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, config.Name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return newSettings(cfg, codeOpts{cfg: cfg, maxLines: 3, maxLine: 100, commits: 20}), path
}

// Пустой languages.only означает "все языки", поэтому все галочки стоят. Первое
// снятие должно развернуть список, иначе фильтр так и остался бы пустым.
func TestSettingsLanguageToggleExpandsList(t *testing.T) {
	s, path := settingsOn(t, "version: 1\ncomments:\n  max_lines: 3\nlanguages:\n  only: []\n")
	for _, l := range config.Langs() {
		if !s.on(l) {
			t.Fatalf("при пустом списке язык %s выключен", l)
		}
	}
	rows := s.rows()
	for i, r := range rows {
		if r == "go" {
			s.idx = i
		}
	}
	s = s.toggle()
	if s.on("go") {
		t.Error("go остался включённым")
	}
	if !s.on("ts") {
		t.Error("вместе с go выключился ts")
	}

	s = s.save()
	if s.err != "" {
		t.Fatal(s.err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Languages.Only) != len(config.Langs())-1 {
		t.Errorf("в файле %v", cfg.Languages.Only)
	}
	for _, l := range cfg.Languages.Only {
		if l == "go" {
			t.Error("go попал в список")
		}
	}
}

// Числа меняются стрелками и записываются в тот же файл, комментарии целы.
func TestSettingsSaveNumbers(t *testing.T) {
	src := "version: 1\n\ncomments:\n  # предел строк\n  max_lines: 3\n"
	s, path := settingsOn(t, src)
	s.idx = 0
	s = s.bump(-1)
	if s.maxLines != 2 {
		t.Fatalf("max_lines = %d", s.maxLines)
	}
	s = s.save()
	if s.err != "" || !s.saved {
		t.Fatalf("сохранение: %q", s.err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "max_lines: 2") {
		t.Errorf("файл:\n%s", got)
	}
	if !strings.Contains(string(got), "# предел строк") {
		t.Error("комментарий стёрт")
	}
}

func TestSettingsEscCloses(t *testing.T) {
	s, _ := settingsOn(t, "version: 1\n")
	if _, closed := s.update(tea.KeyMsg{Type: tea.KeyDown}); closed {
		t.Error("стрелка закрыла экран")
	}
	if _, closed := s.update(tea.KeyMsg{Type: tea.KeyEsc}); !closed {
		t.Error("esc не закрыл экран")
	}
}
