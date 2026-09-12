package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/haiodo/hum1izer/internal/baseline"
	"github.com/haiodo/hum1izer/internal/code"
)

// tuiOnFile собирает модель на настоящем файле: решения пишутся сразу, поэтому
// без файла проверять нечего.
func tuiOnFile(t *testing.T, body string) (tuiModel, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(file)
	if err != nil {
		t.Fatal(err)
	}
	m := tuiModel{root: dir, maxLine: 100, done: map[string]bool{},
		suggest: map[string]string{}, src: map[string][]string{}, width: 120, height: 30}
	for _, c := range cmts {
		m.items = append(m.items, code.Item{Hash: baseline.Hash(c.Text), File: file,
			Start: c.Start, End: c.End, Raw: c.Raw,
			Findings: []code.ItemFinding{{Rule: "R", Fix: "f"}}})
	}
	m.buildTree()
	m.focus = paneBlocks
	return m, file
}

func send(mod tea.Model, s string) tea.Model {
	next, _ := mod.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next
}

// d пишет файл сразу, блок остаётся в списке со словом done, курсор уезжает
// на следующий, а номера строк ниже правки пересчитываются.
func TestDeleteAppliesAtOnce(t *testing.T) {
	m, file := tuiOnFile(t, "package a\n\n// первый комментарий про всё\nx := 1\n\n// второй комментарий про всё\ny := 2\n")
	if len(m.items) != 2 {
		t.Fatalf("блоков %d, ожидалось 2", len(m.items))
	}
	secondStart := m.items[1].Start

	after := send(m, "d").(tuiModel)
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "первый комментарий") {
		t.Error("файл не изменился")
	}
	if !after.done[mark(m.items[0])] {
		t.Error("блок не помечен done")
	}
	if after.blockIdx != 1 {
		t.Errorf("курсор на %d, ожидался 1", after.blockIdx)
	}
	if after.items[1].Start >= secondStart {
		t.Errorf("строка второго блока %d не пересчиталась (была %d)", after.items[1].Start, secondStart)
	}
	if !strings.Contains(after.View(), "done") {
		t.Error("в списке нет пометки done")
	}
}

// y применяет предложение модели сразу, n только убирает его с экрана.
func TestSuggestionApplyAndDiscard(t *testing.T) {
	m, file := tuiOnFile(t, "package a\n\nfunc f() {\n\t// очень длинный комментарий про всё на свете\n\tx := 1\n}\n")
	m.suggest[mark(m.items[0])] = "коротко и по делу"

	discarded := send(m, "n").(tuiModel)
	if len(discarded.suggest) != 0 {
		t.Error("после n предложение осталось")
	}
	if len(discarded.done) != 0 {
		t.Error("n не должен ничего писать")
	}

	m.suggest[mark(m.items[0])] = "коротко и по делу"
	after := send(m, "y").(tuiModel)
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := "package a\n\nfunc f() {\n\t// коротко и по делу\n\tx := 1\n}\n"
	if string(got) != want {
		t.Errorf("файл:\n%q\nожидалось:\n%q\nстатус: %s", got, want, after.status)
	}
	if !after.done[mark(m.items[0])] {
		t.Error("блок не помечен done")
	}
}

// Пробел кладёт блок в снимок и тоже сразу.
func TestKeepWritesBaselineAtOnce(t *testing.T) {
	m, _ := tuiOnFile(t, "package a\n\n// очень длинный комментарий про всё на свете\nx := 1\n")
	m.basePath = filepath.Join(t.TempDir(), "snapshot")

	after := send(m, " ").(tuiModel)
	set, err := baseline.Load(m.basePath)
	if err != nil {
		t.Fatal(err)
	}
	if set.Size() == 0 {
		t.Errorf("снимок пуст, статус: %s", after.status)
	}
	if !after.done[mark(m.items[0])] {
		t.Error("блок не помечен done")
	}
}

// Повторное решение по тому же блоку не должно трогать файл второй раз.
func TestSecondDecisionIsNoop(t *testing.T) {
	m, file := tuiOnFile(t, "package a\n\n// очень длинный комментарий про всё на свете\nx := 1\n")
	after := send(m, "d").(tuiModel)
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	after.blockIdx = 0
	again := send(after, "d").(tuiModel)
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(before) {
		t.Error("второе d снова правило файл")
	}
	if !strings.Contains(again.status, "already done") && !strings.Contains(again.status, "gone") {
		t.Errorf("статус %q", again.status)
	}
}

func TestEditorKeyReturnsCommand(t *testing.T) {
	m, _ := tuiOnFile(t, "package a\n\n// очень длинный комментарий про всё на свете\nx := 1\n")
	t.Setenv("EDITOR", "vi")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if cmd == nil {
		t.Error("e не вернула команду запуска редактора")
	}
}

// Модель должна видеть код вокруг блока, иначе она пересказывает комментарий.
// Проверяем сам запрос: до вызова, без сети.
func TestRewriteRequestCarriesContext(t *testing.T) {
	body := "package a\n\nfunc Prorate(seats int) int {\n" +
		"\t// очень длинный комментарий про пересчёт мест и остаток дней\n" +
		"\tconst dayRate = 30\n\treturn seats * dayRate\n}\n"
	m, _ := tuiOnFile(t, body)
	m.opts.maxLines = 2

	req := m.rewriteRequest(m.items[0])
	if !strings.Contains(req.Comment, "пересчёт мест") {
		t.Errorf("в запросе нет самого комментария: %q", req.Comment)
	}
	for _, want := range []string{"func Prorate(seats int) int {", "const dayRate = 30", "return seats * dayRate"} {
		if !strings.Contains(req.Code, want) {
			t.Errorf("в коде вокруг нет %q:\n%s", want, req.Code)
		}
	}
	if req.Lang != "ru" {
		t.Errorf("язык %q, ожидался ru", req.Lang)
	}
	if len(req.Findings) != 1 || !strings.HasPrefix(req.Findings[0], "R: ") {
		t.Errorf("претензии правил не переданы: %v", req.Findings)
	}
	if req.MaxLines != 2 {
		t.Errorf("предел строк %d, ожидался 2", req.MaxLines)
	}
}

// Шапка показывает выбранную модель и расход токенов после первого вызова.
func TestHeaderShowsModelAndTokens(t *testing.T) {
	m, _ := tuiOnFile(t, "package a\n\n// очень длинный комментарий про всё на свете\nx := 1\n")
	if !strings.Contains(m.header(), "no model") {
		t.Errorf("без клиента шапка: %q", m.header())
	}
	m.llm, _ = newLLMForTest(t, "gpt-5.2")
	m.calls, m.usage.In, m.usage.Out = 2, 12400, 340
	h := m.header()
	for _, want := range []string{"gpt-5.2", "12.4k in", "340 out", "calls: 2", "[m] change"} {
		if !strings.Contains(h, want) {
			t.Errorf("в шапке нет %q: %q", want, h)
		}
	}
}

// Предел строк из настроек - это строки в файле после переноса, а не переводы
// строки в ответе модели: длинная строка развернётся в несколько.
func TestRenderedLinesCountsWrapping(t *testing.T) {
	m, _ := tuiOnFile(t, "package a\n\nfunc f() {\n\t// очень длинный комментарий про всё на свете\n\tx := 1\n}\n")
	m.maxLine = 40
	it := m.items[0]

	if got := m.renderedLines(it, "коротко"); got != 1 {
		t.Errorf("короткий ответ занял %d строк", got)
	}
	if got := m.renderedLines(it, "первая\nвторая"); got != 2 {
		t.Errorf("две строки посчитались как %d", got)
	}
	long := strings.Repeat("слово ", 20)
	if got := m.renderedLines(it, long); got < 3 {
		t.Errorf("длинная строка при maxLine=40 заняла %d строк, ожидалось больше двух", got)
	}
}

// Ответ длиннее предела переспрашивается один раз, потом показывается с
// предупреждением, а не молча применяется.
func TestTooLongSuggestionAsksAgainOnce(t *testing.T) {
	m, _ := tuiOnFile(t, "package a\n\nfunc f() {\n\t// очень длинный комментарий про всё на свете\n\tx := 1\n}\n")
	m.opts.maxLines = 2
	m.llm, _ = newLLMForTest(t, "stub")
	key := mark(m.items[0])
	long := "первая строка\nвторая строка\nтретья строка"

	next, cmd := m.Update(suggestMsg{key: key, text: long})
	if cmd == nil {
		t.Fatal("первый слишком длинный ответ не переспросили")
	}
	if len(next.(tuiModel).suggest) != 0 {
		t.Error("до повтора предложение показывать рано")
	}

	next, cmd = m.Update(suggestMsg{key: key, text: long, retry: true})
	if cmd != nil {
		t.Error("переспросили второй раз")
	}
	after := next.(tuiModel)
	if after.suggest[key] != long {
		t.Error("после повтора предложение не показано")
	}
	if !strings.Contains(after.status, "3 lines") || !strings.Contains(after.status, "limit is 2") {
		t.Errorf("статус без предупреждения: %q", after.status)
	}
}
