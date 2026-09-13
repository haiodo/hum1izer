package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/haiodo/hum1izer/internal/code"
)

// todo.md - журнал за несколько проходов, поэтому вторая заметка дописывается,
// а не заменяет первую, и шапка остаётся одна.
func TestAppendTodoKeepsPreviousNotes(t *testing.T) {
	dir := t.TempDir()
	it := code.Item{File: filepath.Join(dir, "src", "a.ts"), Start: 42,
		Raw:      "// старый комментарий\n// вторая строка",
		Findings: []code.ItemFinding{{Rule: "Длинный комментарий"}, {Rule: "Пересказ кода"}}}

	path, err := appendTodo(dir, "", it, "  выяснить, зачем тут ретрай  ")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "todo.md") {
		t.Errorf("путь %q", path)
	}
	second := code.Item{File: filepath.Join(dir, "b.go"), Start: 7, Raw: "// ещё один"}
	if _, err := appendTodo(dir, "", second, "спросить автора"); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		"# TODO",
		"- [ ] `src/a.ts:42` выяснить, зачем тут ретрай",
		"- правила: Длинный комментарий, Пересказ кода",
		"> // старый комментарий",
		"> // вторая строка",
		"- [ ] `b.go:7` спросить автора",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("нет %q в:\n%s", want, got)
		}
	}
	if strings.Count(got, "# TODO") != 1 {
		t.Errorf("шапка повторилась:\n%s", got)
	}
}

// Пустая заметка не пишется: enter по пустой строке - это отмена.
func TestTodoInputNeedsText(t *testing.T) {
	in := todoInput{}
	in, done, save := in.update(tea.KeyMsg{Type: tea.KeyEnter})
	if !done || save {
		t.Errorf("пустой ввод: done=%v save=%v", done, save)
	}
	in, _, _ = in.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("аб")})
	in, _, _ = in.update(tea.KeyMsg{Type: tea.KeySpace})
	in, _, _ = in.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("в")})
	in, _, _ = in.update(tea.KeyMsg{Type: tea.KeyBackspace})
	if in.text != "аб " {
		t.Errorf("текст %q, ожидалось %q", in.text, "аб ")
	}
	_, done, save = in.update(tea.KeyMsg{Type: tea.KeyEnter})
	if !done || !save {
		t.Errorf("непустой ввод: done=%v save=%v", done, save)
	}
	if _, done, save = in.update(tea.KeyMsg{Type: tea.KeyEsc}); !done || save {
		t.Errorf("esc: done=%v save=%v", done, save)
	}
}

// t на блоке помечает его todo и пишет файл, курсор уезжает дальше.
func TestTodoKeyWritesFile(t *testing.T) {
	m, _ := tuiOnFile(t, "package a\n\n// очень длинный комментарий про всё на свете\nx := 1\n")
	m.noted = map[string]bool{}
	next := send(m, "t").(tuiModel)
	if next.note == nil {
		t.Fatal("ввод заметки не открылся")
	}
	after, _ := next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("проверить")})
	after, _ = after.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := after.(tuiModel)
	if mm.note != nil {
		t.Error("ввод не закрылся")
	}
	if !mm.noted[mark(m.items[0])] {
		t.Errorf("блок не помечен todo, статус %q", mm.status)
	}
	raw, err := os.ReadFile(filepath.Join(m.root, "todo.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "проверить") {
		t.Errorf("todo.md:\n%s", raw)
	}
}
