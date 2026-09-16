package cli

import (
	"strings"
	"testing"

	"github.com/haiodo/hum1izer/internal/code"
)

func tuiItem(hash, file string, start int, rules ...string) code.Item {
	it := code.Item{Hash: hash, File: file, Start: start, Raw: "// " + hash}
	for _, r := range rules {
		it.Findings = append(it.Findings, code.ItemFinding{Rule: r, Fix: "fix " + r})
	}
	return it
}

// Дерево: правило -> файл -> блоки. Блок с двумя правилами виден под обоими,
// правила идут по убыванию количества.
func TestTUITreeGroupsByRuleThenFile(t *testing.T) {
	m := tuiModel{}
	m.items = []code.Item{
		tuiItem("h1", "a.go", 10, "Long comment", "Comment restates the code"),
		tuiItem("h2", "a.go", 20, "Long comment"),
		tuiItem("h3", "b.go", 5, "Long comment"),
	}
	m.buildTree()

	if len(m.rules) != 2 {
		t.Fatalf("правил %d, ожидалось 2", len(m.rules))
	}
	if m.rules[0].name != "Long comment" || m.rules[0].n != 3 {
		t.Errorf("первым идёт %q с %d, ожидалось Long comment с 3", m.rules[0].name, m.rules[0].n)
	}
	if len(m.rules[0].files) != 2 || m.rules[0].files[0].path != "a.go" {
		t.Errorf("файлы правила: %+v", m.rules[0].files)
	}
	if len(m.rules[0].files[0].items) != 2 {
		t.Errorf("в a.go блоков %d, ожидалось 2", len(m.rules[0].files[0].items))
	}
	if m.rules[1].name != "Comment restates the code" || m.rules[1].n != 1 {
		t.Errorf("второе правило: %q %d", m.rules[1].name, m.rules[1].n)
	}
}

// Смена правила сбрасывает выбор файла и блока: индексы от прошлого правила
// указывают в пустоту.
func TestTUIMoveResetsNestedCursors(t *testing.T) {
	m := tuiModel{}
	m.items = []code.Item{
		tuiItem("h1", "a.go", 10, "R1"),
		tuiItem("h2", "b.go", 20, "R1"),
		tuiItem("h3", "c.go", 30, "R2"),
	}
	m.buildTree()
	m.focus = paneFiles
	m.move(1)
	m.focus = paneRules
	m.move(1)
	if m.fileIdx != 0 || m.blockIdx != 0 {
		t.Errorf("после смены правила file=%d block=%d, ожидались нули", m.fileIdx, m.blockIdx)
	}
}

func TestTUIViewRenders(t *testing.T) {
	m := tuiModel{width: 120, height: 30}
	m.items = []code.Item{tuiItem("h1", "a.go", 10, "Long comment")}
	m.buildTree()
	out := m.View()
	for _, want := range []string{"rules", "files", "blocks", "Long comment", "a.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет %q", want)
		}
	}
}

// Синтаксис перехода на строку у редакторов разный, а $EDITOR может быть с
// аргументами. Пустая переменная - не повод ничего не делать: на машине есть vi.
func TestEditorCmd(t *testing.T) {
	cases := []struct {
		editor string
		want   []string
	}{
		{"vim", []string{"+42", "a.go"}},
		{"nano", []string{"+42", "a.go"}},
		{"code -w", []string{"-w", "-g", "a.go:42"}},
		{"subl", []string{"a.go:42"}},
		{"/usr/local/bin/emacs", []string{"+42", "a.go"}},
		{"ed", []string{"a.go"}},
	}
	for _, c := range cases {
		t.Setenv("EDITOR", c.editor)
		name, args := editorCmd("a.go", 42)
		if name == "" {
			t.Errorf("%q: имя пустое", c.editor)
			continue
		}
		if strings.Join(args, " ") != strings.Join(c.want, " ") {
			t.Errorf("%q: аргументы %v, ожидались %v", c.editor, args, c.want)
		}
	}

	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	if name, _ := editorCmd("a.go", 1); name == "" {
		t.Error("без $EDITOR не нашёлся ни один запасной редактор")
	}
}
