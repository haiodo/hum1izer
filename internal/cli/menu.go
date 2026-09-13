package cli

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/haiodo/hum1izer/internal/code"
)

// menu - список действий поверх панелей. Клавиши действий остаются, но помнить
// их больше не надо: Enter на файле открывает то же самое списком.
type menu struct {
	title string
	items []menuItem
	idx   int
}

type menuItem struct {
	id    string
	label string
	hint  string
}

func (mn menu) update(msg tea.KeyMsg) (menu, string, bool) {
	switch msg.String() {
	case "esc", "q":
		return mn, "", true
	case "up", "k":
		mn.idx = clampIdx(mn.idx-1, len(mn.items))
	case "down", "j":
		mn.idx = clampIdx(mn.idx+1, len(mn.items))
	case "enter":
		return mn, mn.items[mn.idx].id, true
	}
	return mn, "", false
}

func (mn menu) View(width int) string {
	var b strings.Builder
	b.WriteString(tuiTitle.Render(" "+mn.title+" ") + "\n\n")
	for i, it := range mn.items {
		line := fmt.Sprintf("  %-28s %s", it.label, tuiFaint.Render(it.hint))
		if i == mn.idx {
			line = tuiCursor.Render(fmt.Sprintf("> %-28s", it.label)) + " " + tuiFaint.Render(it.hint)
		}
		b.WriteString(trimTo(line, width) + "\n")
	}
	b.WriteString("\n" + tuiFaint.Render("j/k move   enter choose   esc cancel"))
	return b.String()
}

// confirmBox - да, нет, отмена стрелками. Нужен там, где действие стоит денег
// или трогает много блоков сразу.
type confirmBox struct {
	text   string
	action string
	idx    int
}

var confirmChoices = []string{"Yes", "No", "Cancel"}

func (c confirmBox) update(msg tea.KeyMsg) (confirmBox, string, bool) {
	switch msg.String() {
	case "esc":
		return c, "", true
	case "left", "h", "up", "k":
		c.idx = clampIdx(c.idx-1, len(confirmChoices))
	case "right", "l", "down", "j":
		c.idx = clampIdx(c.idx+1, len(confirmChoices))
	case "enter":
		return c, confirmChoices[c.idx], true
	}
	return c, "", false
}

func (c confirmBox) View(width int) string {
	var b strings.Builder
	b.WriteString(tuiTitle.Render(" confirm ") + "\n\n")
	b.WriteString(trimTo("  "+c.text, width) + "\n\n  ")
	for i, ch := range confirmChoices {
		if i == c.idx {
			b.WriteString(tuiCursor.Render(" "+ch+" ") + "  ")
			continue
		}
		b.WriteString(" " + ch + "   ")
	}
	b.WriteString("\n\n" + tuiFaint.Render("arrows move   enter choose   esc cancel"))
	return b.String()
}

// blockPick - чекбоксы по блокам файла: с чем работать, а что оставить.
type blockPick struct {
	items []code.Item
	on    map[string]bool
	idx   int
}

func (p blockPick) update(msg tea.KeyMsg) (blockPick, bool, bool) {
	switch msg.String() {
	case "esc", "q":
		return p, true, false
	case "up", "k":
		p.idx = clampIdx(p.idx-1, len(p.items))
	case "down", "j":
		p.idx = clampIdx(p.idx+1, len(p.items))
	case " ":
		if p.idx < len(p.items) {
			k := mark(p.items[p.idx])
			if p.on[k] {
				delete(p.on, k)
			} else {
				p.on[k] = true
			}
		}
	case "a":
		for _, it := range p.items {
			p.on[mark(it)] = true
		}
	case "enter":
		return p, true, true
	}
	return p, false, false
}

func (p blockPick) View(width int) string {
	var b strings.Builder
	b.WriteString(tuiTitle.Render(fmt.Sprintf(" pick blocks: %d of %d ", len(p.on), len(p.items))) + "\n\n")
	const h = 14
	off := 0
	if p.idx >= h {
		off = p.idx - h + 1
	}
	for i := off; i < min(off+h, len(p.items)); i++ {
		it := p.items[i]
		box := "[ ]"
		if p.on[mark(it)] {
			box = tuiAdd.Render("[x]")
		}
		first := strings.SplitN(strings.TrimSpace(it.Raw), "\n", 2)[0]
		line := fmt.Sprintf("  %s %5d  %s", box, it.Start, first)
		if i == p.idx {
			line = tuiCursor.Render(trimTo(fmt.Sprintf("> %s %5d  %s", box, it.Start, first), width))
		}
		b.WriteString(trimTo(line, width) + "\n")
	}
	b.WriteString("\n" + tuiFaint.Render("space toggle   a all   enter go on   esc cancel"))
	return b.String()
}

// scopeItems - с чем работает меню: отмеченные блоки, блоки отмеченных файлов,
// всё правило целиком или текущий файл. Сделанное отсеивается.
func (m tuiModel) scopeItems() []code.Item {
	var src []code.Item
	switch {
	case len(m.picked) > 0:
		for _, it := range m.items {
			if m.picked[mark(it)] {
				src = append(src, it)
			}
		}
	case len(m.selected) == 0 && m.focus == paneRules:
		for _, f := range m.curFiles() {
			src = append(src, f.items...)
		}
	default:
		src = m.pickedItems()
	}
	out := src[:0:0]
	for _, it := range src {
		if !m.done[mark(it)] {
			out = append(out, it)
		}
	}
	return out
}

// openMenu собирает список действий под текущий охват.
func (m tuiModel) openMenu() tea.Model {
	scope := m.scopeItems()
	if len(scope) == 0 {
		m.status = "nothing left here"
		return m
	}
	where := fmt.Sprintf("%d blocks in this file", len(scope))
	switch {
	case len(m.picked) > 0:
		where = fmt.Sprintf("%d picked blocks", len(scope))
	case len(m.selected) > 0:
		where = fmt.Sprintf("%d blocks in %d selected files", len(scope), len(m.selected))
	case m.focus == paneRules && m.ruleIdx < len(m.rules):
		where = fmt.Sprintf("%d blocks of %q", len(scope), m.rules[m.ruleIdx].name)
	}
	m.menu = &menu{title: where, items: []menuItem{
		{"rewrite", "Rewrite with the model", "a batch, then review"},
		{"review", "Review suggestions", "one by one, y/n"},
		{"keepall", "Keep all", "into the baseline"},
		{"pick", "Pick blocks", "checkboxes"},
		{"clear", "Clear selection", "files and blocks"},
	}}
	return m
}

func (m tuiModel) menuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	mn, id, done := m.menu.update(msg)
	if !done {
		m.menu = &mn
		return m, nil
	}
	m.menu = nil
	scope := m.scopeItems()
	switch id {
	case "rewrite":
		m.confirm = &confirmBox{action: id,
			text: fmt.Sprintf("Ask the model to rewrite %d blocks?", len(scope))}
	case "review":
		keys := m.reviewKeys()
		if len(keys) == 0 {
			m.status = "no suggestions yet"
			return m, nil
		}
		m.review = &review{keys: keys}
	case "keepall":
		m.confirm = &confirmBox{action: id,
			text: fmt.Sprintf("Keep %d blocks as they are?", len(scope))}
	case "pick":
		m.pick = &blockPick{items: m.scopeFile(), on: map[string]bool{}}
		for k := range m.picked {
			m.pick.on[k] = true
		}
	case "clear":
		m.selected, m.picked = map[string]bool{}, map[string]bool{}
		m.status = "selection cleared"
	}
	return m, nil
}

// scopeFile - все блоки файла под курсором: чекбоксы ставятся по файлу, а не
// по всему отмеченному разом.
func (m tuiModel) scopeFile() []code.Item {
	var out []code.Item
	for _, it := range m.curItems() {
		if !m.done[mark(it)] {
			out = append(out, it)
		}
	}
	return out
}

func (m tuiModel) pickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p, done, ok := m.pick.update(msg)
	if !done {
		m.pick = &p
		return m, nil
	}
	m.pick = nil
	if !ok {
		return m, nil
	}
	m.picked = p.on
	m.status = fmt.Sprintf("picked blocks: %d", len(m.picked))
	return m.openMenu(), nil
}

func (m tuiModel) confirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c, answer, done := m.confirm.update(msg)
	if !done {
		m.confirm = &c
		return m, nil
	}
	action := m.confirm.action
	m.confirm = nil
	if answer != "Yes" {
		return m, nil
	}
	switch action {
	case "rewrite":
		return m.rewriteScope()
	case "keepall":
		return m.keepScope(), nil
	}
	return m, nil
}

func (m tuiModel) rewriteScope() (tea.Model, tea.Cmd) {
	if m.llm == nil {
		m.status = "rewrite unavailable: " + m.llmErr
		return m, nil
	}
	var queue []code.Item
	for _, it := range m.scopeItems() {
		if m.suggest[mark(it)] == "" {
			queue = append(queue, it)
		}
	}
	if len(queue) == 0 {
		m.status = "every block here already has a suggestion"
		return m, nil
	}
	m.queue = append(m.queue, queue...)
	m.batch = true
	m.status = fmt.Sprintf("asking the model for %d blocks...", len(queue))
	return m.pump()
}

// maxInFlight - сколько запросов к модели идёт одновременно. Категория целиком
// это сотни блоков, а лимиты у эндпоинтов свои и наказание за превышение тоже.
const maxInFlight = 5

// pump шлёт из очереди столько, сколько влезает в лимит, а по концу пачки сам
// открывает разбор: решать по одному, пока остальные считаются, неудобно.
func (m tuiModel) pump() (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for m.pending < maxInFlight && len(m.queue) > 0 {
		it := m.queue[0]
		m.queue = m.queue[1:]
		if m.done[mark(it)] || m.suggest[mark(it)] != "" {
			continue
		}
		m.pending++
		cmds = append(cmds, m.rewriteCmd(it))
	}
	if len(cmds) > 0 {
		return m, tea.Batch(cmds...)
	}
	if m.batch && m.pending == 0 && len(m.queue) == 0 {
		m.batch = false
		if keys := m.reviewKeys(); len(keys) > 0 {
			m.review = &review{keys: keys}
			m.status = fmt.Sprintf("%d suggestions ready", len(keys))
		}
	}
	return m, nil
}

func (m tuiModel) keepScope() tea.Model {
	if m.basePath == "" {
		m.status = "nowhere to keep: set --baseline or baseline in .hum1izer.yaml"
		return m
	}
	n := 0
	for _, it := range m.scopeItems() {
		next, ok := m.applyItem(it, markKeep, "").(tuiModel)
		if !ok {
			break
		}
		m = next
		n++
	}
	m.status = fmt.Sprintf("kept %d blocks", n)
	return m
}
