package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/haiodo/hum1izer/internal/baseline"
	"github.com/haiodo/hum1izer/internal/code"
)

func menuModel(t *testing.T, files ...string) tuiModel {
	t.Helper()
	dir := t.TempDir()
	body := "package a\n\n// первый комментарий про всё на свете\nx := 1\n\n// второй комментарий про всё\ny := 2\n"
	var items []code.Item
	for _, name := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cmts, err := code.ExtractComments(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cmts {
			items = append(items, code.Item{Hash: baseline.Hash(c.Text), File: p,
				Start: c.Start, End: c.End, Raw: c.Raw,
				Findings: []code.ItemFinding{{Rule: "R", Fix: "f"}}})
		}
	}
	m := tuiModel{root: dir, maxLine: 100, opts: codeOpts{maxLines: 2, maxLine: 100},
		items: items, done: map[string]bool{},
		suggest: map[string]string{}, noted: map[string]bool{}, selected: map[string]bool{},
		picked: map[string]bool{}, src: map[string][]string{}, width: 120, height: 30}
	m.buildTree()
	return m
}

// Enter открывает меню, а не выполняет действие вслепую. Охват в заголовке
// считается по отмеченным файлам.
func TestEnterOpensMenuWithScope(t *testing.T) {
	m := menuModel(t, "a.go", "b.go")
	m.focus = paneFiles
	m = send(m, " ").(tuiModel)
	m.fileIdx = 1
	m = send(m, " ").(tuiModel)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := next.(tuiModel)
	if mm.menu == nil {
		t.Fatal("меню не открылось")
	}
	if !strings.Contains(mm.menu.title, "4 blocks in 2 selected files") {
		t.Errorf("заголовок меню: %q", mm.menu.title)
	}
	if !strings.Contains(mm.View(), "Rewrite with the model") {
		t.Error("пункты меню не нарисованы")
	}
}

// Keep all спрашивает подтверждение, а не пишет сразу. No и Cancel ничего не
// делают, Yes кладёт весь охват в снимок.
func TestKeepAllAsksConfirm(t *testing.T) {
	m := menuModel(t, "a.go")
	m.basePath = filepath.Join(t.TempDir(), "snapshot")
	m.menu = &menu{title: "x", items: []menuItem{{"keepall", "Keep all", ""}}}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := next.(tuiModel)
	if mm.confirm == nil || !strings.Contains(mm.confirm.text, "Keep 2 blocks") {
		t.Fatalf("подтверждение: %+v", mm.confirm)
	}

	// No: ничего не записано
	no, _ := mm.Update(tea.KeyMsg{Type: tea.KeyRight})
	no, _ = no.(tuiModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(no.(tuiModel).done) != 0 {
		t.Error("ответ No всё равно применился")
	}

	yes, _ := mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	ym := yes.(tuiModel)
	if len(ym.done) != 2 {
		t.Errorf("применено %d блоков, ожидалось 2, статус %q", len(ym.done), ym.status)
	}
	set, err := baseline.Load(m.basePath)
	if err != nil {
		t.Fatal(err)
	}
	if set.Size() == 0 {
		t.Error("снимок пуст")
	}
}

// Чекбоксы сужают охват до отмеченных блоков и возвращают в меню.
func TestPickNarrowsScope(t *testing.T) {
	m := menuModel(t, "a.go")
	m.pick = &blockPick{items: m.curItems(), on: map[string]bool{}}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	next, _ = next.(tuiModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := next.(tuiModel)
	if len(mm.picked) != 1 {
		t.Fatalf("отмечено %d блоков, ожидался 1", len(mm.picked))
	}
	if len(mm.scopeItems()) != 1 {
		t.Errorf("охват %d, ожидался 1", len(mm.scopeItems()))
	}
	if mm.menu == nil || !strings.Contains(mm.menu.title, "1 picked blocks") {
		t.Errorf("меню после выбора: %+v", mm.menu)
	}
}

// Нижняя строка показывает только то, что работает здесь и сейчас: пункты меню
// в ней не дублируются, а y/n появляются только при готовом предложении.
func TestHelpFollowsContext(t *testing.T) {
	m := menuModel(t, "a.go")
	m.width = 120

	m.focus = paneRules
	rules := m.help()
	for _, gone := range []string{"space", "d delete", "y accept", "A batch", "R review", "a rewrite"} {
		if strings.Contains(rules, gone) {
			t.Errorf("в панели правил лишнее %q: %s", gone, rules)
		}
	}
	if !strings.Contains(rules, "enter menu") {
		t.Errorf("нет главного пункта: %s", rules)
	}

	m.focus = paneFiles
	if !strings.Contains(m.help(), "space select file") {
		t.Errorf("панель файлов: %s", m.help())
	}

	m.focus = paneBlocks
	if strings.Contains(m.help(), "y accept") {
		t.Errorf("y/n показаны без предложения: %s", m.help())
	}
	m.suggest[mark(m.items[0])] = "коротко"
	if !strings.Contains(m.help(), "y accept") {
		t.Errorf("y/n не показаны при предложении: %s", m.help())
	}

	// esc снимает выбор, а не выходит, пока что-то отмечено.
	m.selected["a.go"] = true
	if !strings.Contains(m.help(), "esc clear") {
		t.Errorf("нет подсказки про esc: %s", m.help())
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Error("esc с выбором вышел из программы")
	}
	if len(next.(tuiModel).selected) != 0 {
		t.Error("esc не снял выбор")
	}
}

// Меню работает и по правилу целиком: курсор в левой панели - охват это все
// блоки правила, по всем файлам.
func TestMenuScopeCoversWholeRule(t *testing.T) {
	m := menuModel(t, "a.go", "b.go")
	m.focus = paneRules
	if got := len(m.scopeItems()); got != 4 {
		t.Errorf("охват правила %d, ожидалось 4", got)
	}
	mm := m.openMenu().(tuiModel)
	if mm.menu == nil || !strings.Contains(mm.menu.title, `blocks of "R"`) {
		t.Errorf("заголовок меню: %+v", mm.menu)
	}
	// Курсор в панели файлов - охват сужается до файла.
	m.focus = paneFiles
	if got := len(m.scopeItems()); got != 2 {
		t.Errorf("охват файла %d, ожидалось 2", got)
	}
}

// К модели уходит не больше maxInFlight запросов разом, остальное ждёт в
// очереди и досылается по мере ответов.
func TestQueueLimitsParallelRequests(t *testing.T) {
	m := menuModel(t, "a.go", "b.go", "c.go", "d.go")
	m.llm, _ = newLLMForTest(t, "stub")
	m.focus = paneRules
	if got := len(m.scopeItems()); got != 8 {
		t.Fatalf("охват %d, ожидалось 8", got)
	}

	next, cmd := m.rewriteScope()
	mm := next.(tuiModel)
	if cmd == nil {
		t.Fatal("команда не создана")
	}
	if mm.pending != maxInFlight {
		t.Errorf("в полёте %d, ожидалось %d", mm.pending, maxInFlight)
	}
	if len(mm.queue) != 8-maxInFlight {
		t.Errorf("в очереди %d, ожидалось %d", len(mm.queue), 8-maxInFlight)
	}

	// Ответ освобождает место, и очередь двигается.
	after, cmd := mm.Update(suggestMsg{key: mark(mm.items[0]), text: "коротко"})
	am := after.(tuiModel)
	if cmd == nil {
		t.Error("следующий запрос не отправлен")
	}
	if am.pending != maxInFlight {
		t.Errorf("после ответа в полёте %d, ожидалось %d", am.pending, maxInFlight)
	}
	if len(am.queue) != 8-maxInFlight-1 {
		t.Errorf("очередь не сдвинулась: %d", len(am.queue))
	}

	// Ошибка модели тоже двигает очередь, иначе она встанет навсегда.
	errModel, cmd := am.Update(suggestMsg{key: mark(am.items[1]), err: os.ErrDeadlineExceeded})
	if cmd == nil {
		t.Error("после ошибки очередь встала")
	}
	if len(errModel.(tuiModel).queue) != 8-maxInFlight-2 {
		t.Errorf("очередь после ошибки: %d", len(errModel.(tuiModel).queue))
	}
}

// Выход из настроек без записи не должен пересканировать дерево: на большом
// репозитории это секунды, а настройки те же.
func TestSettingsExitWithoutSaveSkipsRescan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("package a\n\n// длинный комментарий про всё на свете\nx := 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := menuModel(t, "a.go")
	st := settings{path: "", maxLines: 2, langs: map[string]bool{}}
	m.cfgEdit = &st
	m.items = nil // rescan вернул бы находки обратно

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm := next.(tuiModel)
	if mm.cfgEdit != nil {
		t.Error("экран не закрылся")
	}
	if len(mm.items) != 0 {
		t.Errorf("прогон был, хотя ничего не меняли: %d блоков", len(mm.items))
	}

	// После записи прогон нужен.
	st2 := settings{path: "", maxLines: 2, langs: map[string]bool{}, saved: true}
	m.cfgEdit = &st2
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(next.(tuiModel).status, "settings reloaded") {
		t.Errorf("после записи прогона не было: %q", next.(tuiModel).status)
	}
}

// Пачка сама открывает разбор, когда пришёл последний ответ, а не раньше.
func TestBatchOpensReviewWhenDrained(t *testing.T) {
	m := menuModel(t, "a.go")
	m.llm, _ = newLLMForTest(t, "stub")
	next, _ := m.rewriteScope()
	mm := next.(tuiModel)
	if mm.pending != 2 || !mm.batch {
		t.Fatalf("старт пачки: pending %d, batch %v", mm.pending, mm.batch)
	}

	after, _ := mm.Update(suggestMsg{key: mark(mm.items[0]), text: "коротко"})
	am := after.(tuiModel)
	if am.review != nil {
		t.Error("разбор открылся, пока пачка ещё идёт")
	}
	after, _ = am.Update(suggestMsg{key: mark(am.items[1]), text: "тоже коротко"})
	am = after.(tuiModel)
	if am.review == nil || len(am.review.keys) != 2 {
		t.Errorf("разбор не открылся по концу пачки: %+v", am.review)
	}
	if am.batch {
		t.Error("флаг пачки не снят")
	}
}

// Отказ по лимиту возвращает блок в очередь и ставит паузу, а не теряет его.
func TestRateLimitRequeues(t *testing.T) {
	m := menuModel(t, "a.go", "b.go", "c.go", "d.go")
	m.llm, _ = newLLMForTest(t, "stub")
	m.focus = paneRules
	next, _ := m.rewriteScope()
	mm := next.(tuiModel)
	was := len(mm.queue)

	limited := errors.New(`POST "/v1/chat/completions": 429 Too Many Requests {"message":"rate limit 12/min reached — retry in 7s."}`)
	after, cmd := mm.Update(suggestMsg{key: mark(mm.items[0]), err: limited})
	am := after.(tuiModel)
	if cmd == nil {
		t.Fatal("пауза не запланирована")
	}
	if len(am.queue) != was+1 {
		t.Errorf("в очереди %d, ожидалось %d: блок потерян", len(am.queue), was+1)
	}
	if mark(am.queue[0]) != mark(mm.items[0]) {
		t.Error("блок вернулся не в начало очереди")
	}
	if !strings.Contains(am.status, "rate limit") || !strings.Contains(am.status, "8s") {
		t.Errorf("статус: %q", am.status)
	}
	// Пока ждём, новых запросов не шлём.
	if am.pending >= mm.pending {
		t.Errorf("слот не освободился: было %d, стало %d", mm.pending, am.pending)
	}
}

func actIdx(id string) int {
	for i, a := range reviewActs {
		if a.id == id {
			return i
		}
	}
	return 0
}

// В разборе действие выбирается стрелками и запускается по enter; y, n, r
// остаются как сокращения.
func TestReviewActionsByArrows(t *testing.T) {
	m := menuModel(t, "a.go")
	for _, it := range m.items {
		m.suggest[mark(it)] = "коротко"
	}
	m.review = &review{keys: m.reviewKeys()}

	// Вправо: Accept -> Reject, enter отбрасывает предложение и не пишет файл.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	rm := next.(tuiModel)
	if rm.review.act != 1 {
		t.Fatalf("действие %d, ожидалось 1", rm.review.act)
	}
	next, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am := next.(tuiModel)
	if len(am.done) != 0 {
		t.Error("Reject записал правку")
	}
	if len(am.suggest) != 1 {
		t.Errorf("предложений осталось %d, ожидалось 1", len(am.suggest))
	}

	// Влево возвращает на Accept, enter применяет.
	next, _ = am.Update(tea.KeyMsg{Type: tea.KeyLeft})
	next, _ = next.(tuiModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	ym := next.(tuiModel)
	if len(ym.done) != 1 {
		t.Errorf("Accept не применил: done %d, статус %q", len(ym.done), ym.status)
	}

	// Skip не трогает ни файл, ни предложение.
	m2 := menuModel(t, "b.go")
	for _, it := range m2.items {
		m2.suggest[mark(it)] = "коротко"
	}
	m2.review = &review{keys: m2.reviewKeys(), act: actIdx("skip")}
	next, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	sm := next.(tuiModel)
	if len(sm.done) != 0 || len(sm.suggest) != 2 {
		t.Errorf("Skip что-то сделал: done %d, suggest %d", len(sm.done), len(sm.suggest))
	}
	if sm.review.idx != 1 {
		t.Errorf("Skip не перешёл к следующему: idx %d", sm.review.idx)
	}
}

// В разборе есть и удаление: комментарий бывает не переписать, а выбросить.
func TestReviewDeletesBlock(t *testing.T) {
	m := menuModel(t, "a.go")
	file := m.items[0].File
	for _, it := range m.items {
		m.suggest[mark(it)] = "коротко"
	}
	m.review = &review{keys: m.reviewKeys()}

	// Доходим до Delete стрелками, как это делает человек.
	next := tea.Model(m)
	for reviewActs[next.(tuiModel).review.act].id != "delete" {
		next, _ = next.(tuiModel).Update(tea.KeyMsg{Type: tea.KeyRight})
	}
	rm := next.(tuiModel)
	next, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm := next.(tuiModel)

	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "первый комментарий") {
		t.Errorf("комментарий не удалён:\n%s", raw)
	}
	if strings.Contains(string(raw), "коротко") {
		t.Error("вместо удаления применено предложение")
	}
	if len(dm.done) != 1 {
		t.Errorf("блок не помечен done: %d, статус %q", len(dm.done), dm.status)
	}
	if len(dm.review.keys) != 1 {
		t.Errorf("блок остался в очереди разбора: %d", len(dm.review.keys))
	}
}
