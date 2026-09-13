package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/haiodo/hum1izer/internal/code"
)

// todoName - файл в корне репозитория, куда уходят заметки из прогона.
const todoName = "todo.md"

const todoHead = "# TODO\n\nЗаметки из прогона hum1izer: блок, к нему вопрос и что с ним делать.\n"

// todoInput - строка ввода поверх панелей. Своя, а не из bubbles: нужен один
// однострочный ввод, ради него зависимость не берём.
type todoInput struct {
	item code.Item
	text string
}

func (t todoInput) update(msg tea.KeyMsg) (todoInput, bool, bool) {
	switch msg.Type {
	case tea.KeyEsc:
		return t, true, false
	case tea.KeyEnter:
		return t, true, strings.TrimSpace(t.text) != ""
	case tea.KeyBackspace:
		if r := []rune(t.text); len(r) > 0 {
			t.text = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		t.text += " "
	case tea.KeyRunes:
		t.text += string(msg.Runes)
	}
	return t, false, false
}

func (t todoInput) View(width int) string {
	var b strings.Builder
	b.WriteString(tuiTitle.Render(" note for todo.md ") + "\n\n")
	b.WriteString(trimTo(fmt.Sprintf("  %s:%d", shortName(t.item.File), t.item.Start), width) + "\n")
	for _, l := range strings.Split(strings.TrimRight(t.item.Raw, "\n"), "\n") {
		b.WriteString(tuiFaint.Render(trimTo("  │ "+l, width)) + "\n")
	}
	b.WriteString("\n  " + t.text + tuiCursor.Render(" ") + "\n\n")
	b.WriteString(tuiFaint.Render("enter save   esc cancel"))
	return b.String()
}

// appendTodo дописывает заметку в todo.md рядом с настройками проекта, иначе в
// корне прогона. Именно дописывает: файл - журнал за несколько проходов.
func appendTodo(root, cfgPath string, it code.Item, note string) (string, error) {
	dir := root
	if cfgPath != "" {
		dir = filepath.Dir(cfgPath)
	}
	path := filepath.Join(dir, todoName)

	body := ""
	if raw, err := os.ReadFile(path); err == nil {
		body = string(raw)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if body == "" {
		body = todoHead
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}

	file := it.File
	if rel, err := filepath.Rel(dir, it.File); err == nil && !strings.HasPrefix(rel, "..") {
		file = rel
	}
	var rules []string
	for _, f := range it.Findings {
		rules = append(rules, f.Rule)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n- [ ] `%s:%d` %s\n", file, it.Start, strings.TrimSpace(note))
	if len(rules) > 0 {
		fmt.Fprintf(&b, "  - правила: %s\n", strings.Join(rules, ", "))
	}
	for _, l := range strings.Split(strings.TrimRight(it.Raw, "\n"), "\n") {
		fmt.Fprintf(&b, "  > %s\n", strings.TrimSpace(l))
	}

	if err := os.WriteFile(path, []byte(body+b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
