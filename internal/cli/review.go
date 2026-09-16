package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/haiodo/hum1izer/internal/code"
)

// review - отдельный экран разбора готовых предложений. Смысл в том, чтобы
// сначала прогнать модель по отмеченным файлам, а решать потом и подряд.
type review struct {
	keys []string // mark(item) в порядке файл, строка
	idx  int
	act  int // выбранное действие в нижнем ряду
}

// reviewActs - ряд действий под диффом. Клавиши y, n, r остались, но помнить
// их не надо: стрелки и enter делают то же самое.
var reviewActs = []struct {
	id, label string
}{
	{"accept", "Accept"},
	{"reject", "Reject"},
	{"delete", "Delete"},
	{"again", "Ask again"},
	{"skip", "Skip"},
}

// reviewKeys - все блоки с готовым предложением, в порядке обхода файлов.
func (m tuiModel) reviewKeys() []string {
	type row struct {
		key   string
		file  string
		start int
	}
	var rows []row
	for _, it := range m.items {
		k := mark(it)
		if m.suggest[k] == "" || m.done[k] {
			continue
		}
		rows = append(rows, row{k, it.File, it.Start})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].file != rows[j].file {
			return rows[i].file < rows[j].file
		}
		return rows[i].start < rows[j].start
	})
	keys := make([]string, 0, len(rows))
	for _, r := range rows {
		keys = append(keys, r.key)
	}
	return keys
}

func (m tuiModel) reviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	r := m.review
	if len(r.keys) == 0 {
		m.review = nil
		return m, nil
	}
	it, ok := m.itemByKey(r.keys[r.idx])
	key := msg.String()
	if key == "enter" {
		key = map[string]string{"accept": "y", "reject": "n", "delete": "d", "again": "r", "skip": "j"}[reviewActs[r.act].id]
	}
	switch key {
	case "esc", "q":
		m.review = nil
	case "left", "h":
		r.act = clampIdx(r.act-1, len(reviewActs))
	case "right", "l", "tab":
		r.act = clampIdx(r.act+1, len(reviewActs))
	case "up", "k":
		r.idx = clampIdx(r.idx-1, len(r.keys))
	case "down", "j":
		r.idx = clampIdx(r.idx+1, len(r.keys))
	case "y":
		if !ok {
			return m, nil
		}
		next := m.applyItem(it, markRewrite, m.suggest[mark(it)]).(tuiModel)
		return next.reviewAdvance(), nil
	case "n":
		if ok {
			delete(m.suggest, mark(it))
		}
		return m.reviewAdvance(), nil
	case "d":
		if !ok {
			return m, nil
		}
		// Удаление блока целиком: предложение модели тут ни при чём, поэтому
		// оно просто снимается вместе с блоком.
		next := m.applyItem(it, markDelete, "").(tuiModel)
		return next.reviewAdvance(), nil
	case "r":
		if !ok || m.llm == nil {
			return m, nil
		}
		// Просим другой вариант, показав прошлый: без него модель повторяет
		// тот же ответ слово в слово.
		req := m.rewriteRequest(it)
		req.Note = "Here's the previous version, it didn't work:\n" + m.suggest[mark(it)] +
			"\nGive another one: same idea, different words."
		key := mark(it)
		client := m.llm
		delete(m.suggest, key)
		m.pending++
		m.status = "asking again..."
		return m, func() tea.Msg {
			text, use, err := client.Rewrite(context.Background(), req)
			return suggestMsg{key: key, text: text, usage: use, retry: true, err: err}
		}
	}
	return m, nil
}

// reviewAdvance убирает решённый блок из очереди и держит курсор на месте.
func (m tuiModel) reviewAdvance() tuiModel {
	keys := m.reviewKeys()
	if len(keys) == 0 {
		m.review = nil
		m.status = "review done, " + fmt.Sprint(len(m.done)) + " applied"
		return m
	}
	idx := clampIdx(m.review.idx, len(keys))
	m.review = &review{keys: keys, idx: idx, act: m.review.act}
	return m
}

func (m tuiModel) reviewView() string {
	r := m.review
	if len(r.keys) == 0 {
		return "nothing to review"
	}
	it, ok := m.itemByKey(r.keys[r.idx])
	if !ok {
		return "block is gone, press esc"
	}
	w := m.width - 2

	var b strings.Builder
	b.WriteString(tuiTitle.Render(fmt.Sprintf(" review %d of %d ", r.idx+1, len(r.keys))))
	if m.pending > 0 {
		b.WriteString("  " + tuiFaint.Render(fmt.Sprintf("%d in flight", m.pending)))
	}
	b.WriteString("\n\n")
	b.WriteString(trimTo(fmt.Sprintf("  %s:%d", m.rel(it.File), it.Start), w) + "\n")
	for _, f := range it.Findings {
		b.WriteString(tuiFaint.Render(trimTo("  → "+f.Rule+": "+f.Fix, w)) + "\n")
	}
	b.WriteString("\n")
	for _, l := range m.blockDetail(it, w) {
		b.WriteString(l + "\n")
	}
	if m.status != "" {
		b.WriteString("\n" + trimTo(m.status, w) + "\n")
	}
	b.WriteString("\n  ")
	for i, a := range reviewActs {
		if i == r.act {
			b.WriteString(tuiCursor.Render(" "+a.label+" ") + "  ")
			continue
		}
		b.WriteString(" " + a.label + "   ")
	}
	b.WriteString("\n\n" + tuiFaint.Render("arrows choose   enter do it   j/k next block   esc back"))
	return b.String()
}

// pickedItems - блоки для прогона модели: из отмеченных файлов, а если ничего
// не отмечено - из того, что под курсором.
func (m tuiModel) pickedItems() []code.Item {
	if len(m.selected) == 0 {
		return m.curItems()
	}
	var out []code.Item
	for _, it := range m.items {
		if m.selected[it.File] {
			out = append(out, it)
		}
	}
	return out
}
