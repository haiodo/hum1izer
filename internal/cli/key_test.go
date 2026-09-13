package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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

// После своей же правки блок меняет границы: переписанный стал короче, у
// удалённого подсвечивать нечего. Иначе стрелки остаются на чужих строках.
func TestEditedBlockBoundsRefresh(t *testing.T) {
	body := "package a\n\nfunc f() {\n\t// первая строка про всё на свете\n\t// вторая строка\n\t// третья строка\n\tx := 1\n}\n"

	m, _ := tuiOnFile(t, body)
	m.suggest[mark(m.items[0])] = "коротко"
	after := send(m, "y").(tuiModel)
	if got := after.items[0].End; got != after.items[0].Start {
		t.Errorf("после замены End=%d, Start=%d - блок стал однострочным", got, after.items[0].Start)
	}
	if !strings.Contains(after.items[0].Raw, "коротко") {
		t.Errorf("Raw не обновился: %q", after.items[0].Raw)
	}

	m2, _ := tuiOnFile(t, body)
	del := send(m2, "d").(tuiModel)
	if del.items[0].End != -1 {
		t.Errorf("после удаления End=%d, ожидалось -1", del.items[0].End)
	}
	// В деталях блока стрелок быть не должно, а код вокруг остаётся.
	lines := del.blockDetail(del.items[0], 120)
	for _, l := range lines {
		if strings.Contains(l, "▸") {
			t.Errorf("стрелка на удалённом блоке: %q", l)
		}
	}
	if len(lines) == 0 {
		t.Error("код вокруг удалённого блока пропал")
	}
}

// Предложение показывается диффом: строки блока уходят под "-", новый текст под
// "+" и ровно в том виде, в каком он ляжет в файл.
func TestSuggestionRendersAsDiff(t *testing.T) {
	m, _ := tuiOnFile(t, "package a\n\nfunc f() {\n\t// первая строка про всё\n\t// вторая строка\n\tx := 1\n}\n")
	m.maxLine = 100
	it := m.items[0]
	m.suggest[mark(it)] = "коротко и по делу"

	var minus, plus []string
	for _, l := range m.blockDetail(it, 200) {
		switch {
		case strings.Contains(l, "  - "):
			minus = append(minus, l)
		case strings.Contains(l, "  + "):
			plus = append(plus, l)
		}
	}
	if len(minus) != 2 {
		t.Errorf("строк '-' %d, ожидалось 2: %q", len(minus), minus)
	}
	if len(plus) != 1 {
		t.Fatalf("строк '+' %d, ожидалось 1: %q", len(plus), plus)
	}
	if !strings.Contains(plus[0], "// коротко и по делу") {
		t.Errorf("новый текст без маркера комментария: %q", plus[0])
	}
	if strings.Contains(strings.Join(m.blockDetail(it, 200), " "), "▸") {
		t.Error("при открытом диффе стрелки лишние")
	}
}

// A просит модель переписать все блоки файла, кроме уже сделанных и тех, где
// предложение уже есть.
func TestRewriteWholeFileSkipsDoneAndSuggested(t *testing.T) {
	body := "package a\n\n// первый комментарий про всё\nx := 1\n\n// второй комментарий про всё\ny := 2\n\n// третий комментарий про всё\nz := 3\n"
	m, _ := tuiOnFile(t, body)
	if len(m.items) != 3 {
		t.Fatalf("блоков %d", len(m.items))
	}
	m.llm, _ = newLLMForTest(t, "stub")
	m.done[mark(m.items[0])] = true
	m.suggest[mark(m.items[1])] = "уже есть"

	next, cmd := m.askRewriteFile()
	if cmd == nil {
		t.Fatal("команда не создана")
	}
	if got := next.(tuiModel).pending; got != 1 {
		t.Errorf("в полёте %d запросов, ожидался 1", got)
	}

	m.done[mark(m.items[2])] = true
	next, cmd = m.askRewriteFile()
	if cmd != nil {
		t.Error("просить нечего, а команда создана")
	}
	if !strings.Contains(next.(tuiModel).status, "nothing left") {
		t.Errorf("статус %q", next.(tuiModel).status)
	}
}

// Пробел в панели файлов отмечает файл, A прогоняет модель по всем отмеченным,
// а не только по текущему.
func TestSelectedFilesDriveBatch(t *testing.T) {
	dir := t.TempDir()
	var items []code.Item
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("package a\n\n// длинный комментарий про всё на свете\nx := 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmts, err := code.ExtractComments(p)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, code.Item{Hash: baseline.Hash(cmts[0].Text), File: p,
			Start: cmts[0].Start, End: cmts[0].End, Raw: cmts[0].Raw,
			Findings: []code.ItemFinding{{Rule: "R", Fix: "f"}}})
	}
	m := tuiModel{root: dir, maxLine: 100, done: map[string]bool{}, suggest: map[string]string{},
		noted: map[string]bool{}, selected: map[string]bool{}, src: map[string][]string{},
		items: items, width: 120, height: 30}
	m.buildTree()
	m.llm, _ = newLLMForTest(t, "stub")

	// без отметок - только текущий файл
	if got := len(m.pickedItems()); got != 1 {
		t.Errorf("без отметок взято %d блоков, ожидался 1", got)
	}

	m.focus = paneFiles
	m = send(m, " ").(tuiModel)
	m.fileIdx = 2
	m = send(m, " ").(tuiModel)
	if len(m.selected) != 2 {
		t.Fatalf("отмечено файлов %d, ожидалось 2", len(m.selected))
	}
	if got := len(m.pickedItems()); got != 2 {
		t.Errorf("по отметкам взято %d блоков, ожидалось 2", got)
	}
	next, cmd := m.askRewriteFile()
	if cmd == nil {
		t.Fatal("команда не создана")
	}
	if got := next.(tuiModel).pending; got != 2 {
		t.Errorf("в полёте %d, ожидалось 2", got)
	}
}

// Экран ревью идёт по готовым предложениям: y применяет и убирает блок из
// очереди, n отбрасывает, esc закрывает.
func TestReviewWalksSuggestions(t *testing.T) {
	body := "package a\n\n// первый комментарий про всё\nx := 1\n\n// второй комментарий про всё\ny := 2\n"
	m, file := tuiOnFile(t, body)
	m.selected = map[string]bool{}
	for _, it := range m.items {
		m.suggest[mark(it)] = "коротко"
	}
	m = send(m, "R").(tuiModel)
	if m.review == nil || len(m.review.keys) != 2 {
		t.Fatalf("очередь ревью: %+v", m.review)
	}

	after := send(m, "y").(tuiModel)
	if len(after.review.keys) != 1 {
		t.Errorf("после y в очереди %d, ожидался 1", len(after.review.keys))
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "// коротко") {
		t.Errorf("правка не записалась:\n%s", raw)
	}

	after = send(after, "n").(tuiModel)
	if after.review != nil {
		t.Errorf("очередь пуста, экран должен закрыться: %+v", after.review)
	}
	if len(after.suggest) != 0 {
		t.Errorf("предложения остались: %v", after.suggest)
	}
}

// Рамка блока /** */ - это не строки прозы. По ней же считает правило про
// длину, и сравнение с ответом модели должно считать так же.
func TestProseLinesIgnoresFrame(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"/**\n * Remove a document. For documents be aware all\n * attached will be removed as well.\n */", 2},
		{"// одна строка", 1},
		{"// первая\n// вторая", 2},
		{"/* однострочный блок */", 1},
		{"/**\n * Sends a message.\n * @param to recipient\n * @returns id\n */", 1},
		{"#!/usr/bin/env node", 1},
	}
	for _, c := range cases {
		if got := proseLines(c.raw); got != c.want {
			t.Errorf("proseLines(%q) = %d, ожидалось %d", c.raw, got, c.want)
		}
	}
}

// Окно ложится поверх панелей, а не вместо них: строки основного экрана выше и
// ниже окна должны остаться.
func TestOverlayKeepsBaseAroundBox(t *testing.T) {
	base := strings.Join([]string{"первая", "вторая", "третья", "четвёртая", "пятая", "шестая", "седьмая"}, "\n")
	got := overlay(base, "окно", 40, 7)
	lines := strings.Split(got, "\n")
	if len(lines) != 7 {
		t.Fatalf("строк %d, ожидалось 7:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[0], "первая") || !strings.Contains(lines[6], "седьмая") {
		t.Errorf("края основного экрана затёрты:\n%s", got)
	}
	if !strings.Contains(got, "окно") {
		t.Error("окна не видно")
	}
	// Окно занимает три строки с рамкой и стоит по центру.
	if !strings.Contains(lines[3], "окно") {
		t.Errorf("окно не по центру:\n%s", got)
	}
}

// По бокам от окна остаётся основной экран, а не пустота.
func TestOverlayKeepsSidesOfLine(t *testing.T) {
	base := strings.Repeat("L", 20) + strings.Repeat("R", 20)
	got := spliceLine(base, "[окно]", 15, 6, 40)
	plain := ansi.Strip(got)
	if !strings.HasPrefix(plain, strings.Repeat("L", 15)) {
		t.Errorf("левая часть срезана: %q", plain)
	}
	if !strings.HasSuffix(plain, strings.Repeat("R", 19)) {
		t.Errorf("правая часть срезана: %q", plain)
	}
	if !strings.Contains(plain, "[окно]") {
		t.Errorf("окна нет: %q", plain)
	}
	if n := ansi.StringWidth(got); n != 40 {
		t.Errorf("ширина строки %d, ожидалось 40: %q", n, plain)
	}
	// Короткая строка добивается пробелами, а не обрезает окно.
	short := ansi.Strip(spliceLine("ab", "[окно]", 15, 6, 40))
	if !strings.Contains(short, "[окно]") || !strings.HasPrefix(short, "ab ") {
		t.Errorf("короткая строка: %q", short)
	}
}

// Новый комментарий встаёт на место старого, поэтому в диффе он должен стоять
// с тем же отступом. В Raw отступа нет - он берётся из строки файла.
func TestSuggestionKeepsIndentOfBlock(t *testing.T) {
	m, _ := tuiOnFile(t, "package a\n\nfunc f() {\n\tif x {\n\t\t// первый комментарий про всё\n\t\ty := 1\n\t}\n}\n")
	m.maxLine = 100
	it := m.items[0]
	m.suggest[mark(it)] = "коротко"

	var minus, plus string
	for _, l := range m.blockDetail(it, 200) {
		switch {
		case strings.Contains(l, "  - "):
			minus = l
		case strings.Contains(l, "  + "):
			plus = l
		}
	}
	if minus == "" || plus == "" {
		t.Fatal("дифф не собрался")
	}
	at := func(s, needle string) int { return strings.Index(ansi.Strip(s), needle) }
	if a, b := at(minus, "//"), at(plus, "//"); a != b {
		t.Errorf("маркеры не совпали по колонке: %d и %d\n%s\n%s", a, b, ansi.Strip(minus), ansi.Strip(plus))
	}
}

// Ключ блока не должен зависеть от номера строки: правка выше сдвигает строки,
// и решения по остальным блокам файла терялись бы вместе с ключом.
func TestMarkSurvivesLineShift(t *testing.T) {
	m, _ := tuiOnFile(t, "package a\n\n// первый комментарий про всё\nx := 1\n\n// второй комментарий про всё\ny := 2\n")
	second := mark(m.items[1])
	wasStart := m.items[1].Start // items общий для копий модели, значение берём до правки
	m.suggest[second] = "коротко"

	after := send(m, "d").(tuiModel) // удаляем первый блок, второй уезжает вверх
	if after.items[1].Start >= wasStart {
		t.Fatal("строка второго блока не сдвинулась, проверять нечего")
	}
	if mark(after.items[1]) != second {
		t.Errorf("ключ сменился: %q -> %q", second, mark(after.items[1]))
	}
	if after.suggest[mark(after.items[1])] == "" {
		t.Error("предложение по второму блоку потерялось после правки первого")
	}
}
