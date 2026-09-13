package cli

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/haiodo/hum1izer/internal/baseline"
	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/config"
	"github.com/haiodo/hum1izer/internal/humanize"
	"github.com/haiodo/hum1izer/internal/llm"
)

const tuiUsage = `hum1izer tui - разобрать находки руками.

  hum1izer tui [путь]     по умолчанию текущий каталог

Три панели: правила, файлы внутри правила, блоки внутри файла.

  tab, shift+tab   next and previous pane
  arrows, j k      move in the list
  d                delete the block
  space            keep it as is, the block goes to the baseline
  a                ask the model to rewrite it
  y                accept the suggestion, n discards it
  m                pick another model, the choice is saved
  c                settings: limits and which languages to scan
  t                write a note about this block into todo.md
  e                open in $EDITOR at the right line
  r                rescan
  q                quit

Every decision is written to disk at once and the block is marked done in the
list. There is nothing to save on exit.

Флаги те же, что у прогона: --baseline, --config, --max-lines, --max-line,
--skip-tests, --only.

Переписывание идёт в любой OpenAI-совместимый адрес - локальный llama.cpp или
vllm, openrouter, сам OpenAI:

  --llm-url    базовый адрес, по умолчанию OPENAI_BASE_URL, иначе api.openai.com
  --llm-model  имя модели, по умолчанию OPENAI_MODEL
  --llm-key    ключ, по умолчанию OPENAI_API_KEY; локальному серверу не нужен

То же самое секцией llm в .hum1izer.yaml: base_url, model, key.
`

const (
	paneRules = iota
	paneFiles
	paneBlocks
)

const (
	markDelete  = 'd'
	markKeep    = 'k'
	markRewrite = 'r'
)

// Палитра intabia2 из packages/theme/styles/_accent-colors.scss, тон тот же.
// Насыщенность снижена с 63% и 83% до 22-26%: в терминале исходные кричат.
const (
	accentBase  = lipgloss.Color("#594c76") // приглушённый фиолетовый, курсор и шапка
	accentHover = lipgloss.Color("#a46595") // приглушённая маджента, активная панель
	accentSoft  = lipgloss.Color("#b3abc4") // серо-лавандовый, сделанное
	accentText  = lipgloss.Color("#e4e2e9")
	borderIdle  = lipgloss.Color("#403c49")
	textFaint   = lipgloss.Color("#817b8e")
)

var (
	tuiTitle  = lipgloss.NewStyle().Bold(true).Background(accentBase).Foreground(accentText).Padding(0, 1)
	tuiActive = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accentHover).Padding(0, 1)
	tuiIdle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(borderIdle).Padding(0, 1)
	tuiCursor = lipgloss.NewStyle().Background(accentBase).Foreground(accentText)
	tuiFaint  = lipgloss.NewStyle().Foreground(textFaint)
	tuiKeepOn = lipgloss.NewStyle().Foreground(accentSoft)
	tuiPane   = lipgloss.NewStyle().Bold(true).Foreground(accentHover)
)

func runTUI(args []string) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, tuiUsage) }
	basePath := fs.String("baseline", "", "baseline file for kept blocks")
	cfgPath := fs.String("config", "", "settings file instead of searching for .hum1izer.yaml")
	noConfig := fs.Bool("no-config", false, "ignore .hum1izer.yaml")
	rulesPath := fs.String("rules", "", "custom rules file")
	maxLines := fs.Int("max-lines", 2, "flag comments longer than this many lines")
	maxLine := fs.Int("max-line", 100, "flag comment lines longer than this many characters")
	skipTests := fs.Bool("skip-tests", false, "skip test files")
	only := fs.String("only", "", "keep only these rules or categories, comma separated")
	llmURL := fs.String("llm-url", "", "base URL of an OpenAI-compatible API")
	llmModel := fs.String("llm-model", "", "model name used for rewriting")
	llmKey := fs.String("llm-key", "", "API key")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	cfg, err := loadConfig(*cfgPath, *noConfig, root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "settings: %v\n", err)
		return 2
	}
	if *only != "" {
		cfg.Rules.Only = nil
		for _, n := range strings.Split(*only, ",") {
			if n = strings.TrimSpace(n); n != "" {
				cfg.Rules.Only = append(cfg.Rules.Only, n)
			}
		}
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if !given["max-lines"] && cfg.Comments.MaxLines != nil {
		*maxLines = *cfg.Comments.MaxLines
	}
	if !given["max-line"] && cfg.Comments.MaxLine != nil {
		*maxLine = *cfg.Comments.MaxLine
	}
	if !given["skip-tests"] && cfg.Comments.SkipTests != nil {
		*skipTests = *cfg.Comments.SkipTests
	}
	if !given["baseline"] && cfg.Baseline != "" {
		*basePath = resolve(cfg, cfg.Baseline)
	}

	// Коммиты в TUI не показываются: править сообщение уже сделанного коммита
	// нечем, а место в списке оно занимает.
	o := codeOpts{cfg: cfg, rules: *rulesPath, commits: 0,
		maxLines: *maxLines, maxLine: *maxLine, skipTests: *skipTests}

	// Размер придёт первым же WindowSizeMsg, но не в каждом терминале: без
	// значения по умолчанию экран остаётся пустым.
	m := tuiModel{root: root, opts: o, basePath: *basePath,
		maxLine: *maxLine, done: map[string]bool{}, src: map[string][]string{},
		suggest: map[string]string{}, noted: map[string]bool{}, width: 100, height: 30}
	m.llm, m.llmErr = newLLM(cfg, *llmURL, *llmModel, *llmKey)
	if code := m.rescan(); code != 0 {
		return code
	}
	if len(m.items) == 0 {
		fmt.Println("no findings")
		return 0
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// ruleNode и fileNode - дерево правило -> файл -> блоки. Один блок попадает в
// каждое правило, которое на нём сработало: список правил и есть навигация.
type ruleNode struct {
	name  string
	n     int
	files []fileNode
}

type fileNode struct {
	path  string
	items []code.Item
}

type tuiModel struct {
	root     string
	opts     codeOpts
	basePath string
	maxLine  int

	items []code.Item
	rules []ruleNode

	focus                      int
	ruleIdx, fileIdx, blockIdx int

	done    map[string]bool     // что уже применено, ключ mark(item)
	src     map[string][]string // строки файлов для показа кода вокруг блока
	suggest map[string]string   // что модель предложила взамен, до принятия
	llm     *llm.Client
	llmErr  string // почему клиента нет: показывается при первой же попытке
	usage   llm.Usage
	calls   int
	picker  *modelPicker    // не nil, пока выбирают модель
	cfgEdit *settings       // не nil, пока открыт экран настроек
	note    *todoInput      // не nil, пока пишут заметку в todo.md
	noted   map[string]bool // блоки, по которым заметка уже есть
	busy    string
	status  string
	width   int
	height  int
}

// newLLM: флаг, потом .hum1izer.yaml, потом переменные окружения. Ошибку не
// возвращаем наверх - без модели TUI работает, просто клавиша a скажет почему.
func newLLM(cfg config.Config, url, model, key string) (*llm.Client, string) {
	u, err := config.LoadUser()
	if err != nil {
		return nil, err.Error()
	}
	c := llm.Config{
		BaseURL: cmp.Or(url, cfg.LLM.BaseURL, u.LLM.BaseURL, os.Getenv("OPENAI_BASE_URL")),
		Model:   cmp.Or(model, cfg.LLM.Model, u.LLM.Model, os.Getenv("OPENAI_MODEL")),
		Key:     cmp.Or(key, cfg.LLM.Key, u.LLM.Key),
	}
	cl, err := llm.New(c)
	if err != nil {
		return nil, err.Error()
	}
	return cl, ""
}

func mark(it code.Item) string { return it.Hash + "\t" + it.File + fmt.Sprint(it.Start) }

func (m *tuiModel) rescan() int {
	items, _, _, exit := collectItems([]string{m.root}, m.opts)
	if exit == 2 && items == nil {
		return 2
	}
	if m.basePath != "" {
		base, err := baseline.Load(m.basePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "baseline: %v\n", err)
			return 2
		}
		kept := items[:0:0]
		for _, it := range items {
			var fresh []code.ItemFinding
			for _, f := range it.Findings {
				if !base.Known(baseline.Entry{Hash: it.Hash, Rule: f.Rule}) {
					fresh = append(fresh, f)
				}
			}
			if len(fresh) > 0 {
				it.Findings = fresh
				kept = append(kept, it)
			}
		}
		items = kept
	}
	m.items = items
	m.src = map[string][]string{}
	m.buildTree()
	return 0
}

func (m *tuiModel) buildTree() {
	if m.src == nil {
		m.src = map[string][]string{}
	}
	if m.done == nil {
		m.done = map[string]bool{}
	}
	byRule := map[string]map[string][]code.Item{}
	for _, it := range m.items {
		seen := map[string]bool{}
		for _, f := range it.Findings {
			if seen[f.Rule] {
				continue
			}
			seen[f.Rule] = true
			if byRule[f.Rule] == nil {
				byRule[f.Rule] = map[string][]code.Item{}
			}
			byRule[f.Rule][it.File] = append(byRule[f.Rule][it.File], it)
		}
	}
	m.rules = nil
	for name, files := range byRule {
		node := ruleNode{name: name}
		for path, items := range files {
			sort.SliceStable(items, func(i, j int) bool { return items[i].Start < items[j].Start })
			node.files = append(node.files, fileNode{path: path, items: items})
			node.n += len(items)
		}
		sort.Slice(node.files, func(i, j int) bool { return node.files[i].path < node.files[j].path })
		m.rules = append(m.rules, node)
	}
	sort.Slice(m.rules, func(i, j int) bool {
		if m.rules[i].n != m.rules[j].n {
			return m.rules[i].n > m.rules[j].n
		}
		return m.rules[i].name < m.rules[j].name
	})
	m.clamp()
}

func (m *tuiModel) clamp() {
	m.ruleIdx = clampIdx(m.ruleIdx, len(m.rules))
	m.fileIdx = clampIdx(m.fileIdx, len(m.curFiles()))
	m.blockIdx = clampIdx(m.blockIdx, len(m.curItems()))
}

func clampIdx(i, n int) int {
	if n == 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	if i < 0 {
		return 0
	}
	return i
}

func (m tuiModel) curFiles() []fileNode {
	if m.ruleIdx >= len(m.rules) {
		return nil
	}
	return m.rules[m.ruleIdx].files
}

func (m tuiModel) curItems() []code.Item {
	f := m.curFiles()
	if m.fileIdx >= len(f) {
		return nil
	}
	return f[m.fileIdx].items
}

func (m tuiModel) curItem() (code.Item, bool) {
	it := m.curItems()
	if m.blockIdx >= len(it) {
		return code.Item{}, false
	}
	return it[m.blockIdx], true
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Нули приходят от терминалов, которые не сообщают размер: с ними
		// экран схлопывается, а значения по умолчанию хотя бы читаются.
		if msg.Width > 0 && msg.Height > 0 {
			m.width, m.height = msg.Width, msg.Height
		}
		return m, nil
	case settingsDone:
		if msg.err != nil {
			m.status = "editor: " + msg.err.Error()
			return m, nil
		}
		m.cfgEdit = nil
		return m.reloadConfig()
	case modelsMsg:
		m.busy = ""
		if msg.err != nil {
			m.status = "models: " + msg.err.Error()
			return m, nil
		}
		cur := ""
		if m.llm != nil {
			cur = m.llm.Model()
		}
		p := modelPicker{models: msg.models, current: cur}
		for i, name := range msg.models {
			if name == cur {
				p.idx = i
			}
		}
		m.picker = &p
		return m, nil
	case suggestMsg:
		m.busy = ""
		m.calls++
		m.usage.In += msg.usage.In
		m.usage.Out += msg.usage.Out
		if msg.err != nil {
			m.status = "model: " + msg.err.Error()
			return m, nil
		}
		if msg.text == "" {
			m.status = "model returned empty text"
			return m, nil
		}
		it, ok := m.itemByKey(msg.key)
		if n := m.renderedLines(it, msg.text); ok && n > m.opts.maxLines {
			if !msg.retry {
				return m, m.retryRewrite(it, msg.text, n)
			}
			m.status = fmt.Sprintf("suggestion is %d lines, limit is %d - check before accepting", n, m.opts.maxLines)
			m.suggest[msg.key] = msg.text
			return m, nil
		}
		m.suggest[msg.key] = msg.text
		m.status = "suggestion ready: y accept, n discard"
		return m, nil
	case editorDone:
		if msg.err != nil {
			m.status = "editor: " + msg.err.Error()
			return m, nil
		}
		_ = m.rescan()
		m.status = "findings after edit: " + fmt.Sprint(len(m.items))
		return m, nil
	case tea.KeyMsg:
		switch {
		case m.picker != nil:
			return m.pickerKey(msg)
		case m.cfgEdit != nil:
			return m.settingsKey(msg)
		case m.note != nil:
			return m.noteKey(msg)
		}
		return m.key(msg)
	}
	return m, nil
}

// pickerKey: пока открыт список моделей, клавиши уходят ему. Выбор сразу
// пишется в файл пользователя и меняет клиента - иначе он остался бы на старой.
func (m tuiModel) pickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	next, _ := m.picker.Update(msg)
	p := next.(modelPicker)
	if p.chosen == "" && msg.String() != "enter" {
		if s := msg.String(); s == "q" || s == "esc" || s == "ctrl+c" {
			m.picker = nil
			return m, nil
		}
		m.picker = &p
		return m, nil
	}
	m.picker = nil
	if p.chosen == "" {
		return m, nil
	}
	u, err := config.LoadUser()
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	u.LLM.Model = p.chosen
	if _, err := config.SaveUser(u); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.llm, m.llmErr = newLLM(m.opts.cfg, "", p.chosen, "")
	m.status = "model: " + p.chosen
	return m, nil
}

type editorDone struct{ err error }

func (m tuiModel) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "tab", "right", "l":
		m.focus = (m.focus + 1) % 3
	case "shift+tab", "left", "h":
		m.focus = (m.focus + 2) % 3
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-10)
	case "pgdown":
		m.move(10)
	case "d":
		return m.applyOne(markDelete, ""), nil
	case " ":
		return m.applyOne(markKeep, ""), nil
	case "a":
		return m.askRewrite()
	case "y":
		if it, ok := m.curItem(); ok && m.suggest[mark(it)] != "" {
			return m.applyOne(markRewrite, m.suggest[mark(it)]), nil
		}
	case "n":
		if it, ok := m.curItem(); ok {
			delete(m.suggest, mark(it))
			m.status = "suggestion discarded"
		}
	case "m":
		return m.askModels()
	case "c":
		st := newSettings(m.opts.cfg, m.opts)
		m.cfgEdit = &st
	case "t":
		if it, ok := m.curItem(); ok {
			m.note = &todoInput{item: it}
		}
	case "e":
		return m.openEditor()
	case "r":
		_ = m.rescan()
		m.status = "rescanned"
	}
	return m, nil
}

// applyOne пишет решение на диск сразу: копить пометки незачем. Блок остаётся
// в списке помеченным done, поэтому список под курсором не прыгает.
func (m tuiModel) applyOne(kind byte, body string) tea.Model {
	it, ok := m.curItem()
	if !ok {
		return m
	}
	key := mark(it)
	if m.done[key] {
		m.status = "already done"
		return m
	}

	cmts, err := code.ExtractComments(it.File)
	if err != nil {
		m.status = err.Error()
		return m
	}
	raw, err := os.ReadFile(it.File)
	if err != nil {
		m.status = err.Error()
		return m
	}
	var found *code.Comment
	for i := range cmts {
		if baseline.Hash(cmts[i].Text) == it.Hash {
			found = &cmts[i]
			break
		}
	}
	if found == nil {
		m.status = "block is gone from the file, press r to rescan"
		return m
	}

	switch kind {
	case markKeep:
		if m.basePath == "" {
			m.status = "nowhere to keep: set --baseline or baseline in .hum1izer.yaml"
			return m
		}
		rules := make([]string, 0, len(it.Findings))
		for _, f := range it.Findings {
			rules = append(rules, f.Rule)
		}
		if err := keepOne(m.basePath, it.Hash, rules); err != nil {
			m.status = err.Error()
			return m
		}
		m.status = "kept in baseline"
	default:
		e, err := plan(*found, string(raw), kind == markDelete, body, m.maxLine)
		if err != nil {
			m.status = err.Error()
			return m
		}
		if err := writeEdits(it.File, []edit{e}); err != nil {
			m.status = err.Error()
			return m
		}
		m.status = "deleted"
		if kind == markRewrite {
			m.status = "rewritten"
		}
	}

	m.done[key] = true
	delete(m.suggest, key)
	m.refreshEdited(key, found.Start, kind == markDelete)
	m.refreshFile(it.File)
	if m.blockIdx < len(m.curItems())-1 {
		m.blockIdx++
	}
	m.status += fmt.Sprintf("  (done %d of %d)", len(m.done), len(m.items))
	return m
}

// refreshEdited поправляет только что исправленный блок. По хэшу его уже не
// найти - текст другой, - а старый End оставит стрелки на чужих строках.
func (m *tuiModel) refreshEdited(key string, start int, deleted bool) {
	for i := range m.items {
		if mark(m.items[i]) != key {
			continue
		}
		if deleted {
			m.items[i].End = -1
			return
		}
		cmts, err := code.ExtractComments(m.items[i].File)
		if err != nil {
			return
		}
		for _, c := range cmts {
			if c.Start == start {
				m.items[i].End, m.items[i].Raw = c.End, c.Raw
				return
			}
		}
		m.items[i].End = -1
		return
	}
}

// refreshFile: правка сдвинула строки ниже, поэтому у остальных блоков этого
// файла обновляются номера. Ищем по хэшу - текст блока и есть его ключ.
func (m *tuiModel) refreshFile(path string) {
	delete(m.src, path)
	cmts, err := code.ExtractComments(path)
	if err != nil {
		return
	}
	byHash := map[string]code.Comment{}
	for _, c := range cmts {
		byHash[baseline.Hash(c.Text)] = c
	}
	for i := range m.items {
		if m.items[i].File != path {
			continue
		}
		if c, ok := byHash[m.items[i].Hash]; ok {
			m.items[i].Start, m.items[i].End = c.Start, c.End
			continue
		}
		// Хэша нет - блок либо удалён, либо переписан: границы уже выставил
		// refreshEdited, трогать их нельзя.
		m.done[mark(m.items[i])] = true
	}
	m.buildTree()
}

func (m *tuiModel) move(d int) {
	switch m.focus {
	case paneRules:
		m.ruleIdx = clampIdx(m.ruleIdx+d, len(m.rules))
		m.fileIdx, m.blockIdx = 0, 0
	case paneFiles:
		m.fileIdx = clampIdx(m.fileIdx+d, len(m.curFiles()))
		m.blockIdx = 0
	case paneBlocks:
		m.blockIdx = clampIdx(m.blockIdx+d, len(m.curItems()))
	}
}

type suggestMsg struct {
	key   string
	text  string
	usage llm.Usage
	retry bool // ответ уже переспрашивали, второй раз не просим
	err   error
}

type modelsMsg struct {
	models []string
	err    error
}

// askRewrite отправляет блок вместе с кодом вокруг: без кода модель пересказывает
// комментарий, а не объясняет, зачем он.
func (m tuiModel) askRewrite() (tea.Model, tea.Cmd) {
	it, ok := m.curItem()
	if !ok {
		return m, nil
	}
	if m.llm == nil {
		m.status = "rewrite unavailable: " + m.llmErr
		return m, nil
	}
	if m.busy != "" {
		return m, nil
	}

	req := m.rewriteRequest(it)
	key := mark(it)
	client := m.llm
	m.busy = "asking the model..."
	m.status = m.busy
	return m, func() tea.Msg {
		text, use, err := client.Rewrite(context.Background(), req)
		return suggestMsg{key: key, text: text, usage: use, err: err}
	}
}

// noteKey ведёт ввод заметки: enter пишет строку в todo.md, esc отменяет.
func (m tuiModel) noteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	in, done, save := m.note.update(msg)
	if !done {
		m.note = &in
		return m, nil
	}
	m.note = nil
	if !save {
		return m, nil
	}
	path, err := appendTodo(m.root, m.opts.cfg.Path, in.item, in.text)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.noted[mark(in.item)] = true
	m.status = "written to " + shortName(path)
	if m.blockIdx < len(m.curItems())-1 {
		m.blockIdx++
	}
	return m, nil
}

// settingsKey: пока открыт экран настроек, клавиши уходят ему. На закрытии
// настройки перечитываются и дерево сканируется заново.
func (m tuiModel) settingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "e" {
		if m.cfgEdit.path == "" {
			m.cfgEdit.err = "no .hum1izer.yaml here, run: hum1izer init"
			return m, nil
		}
		name, args := editorCmd(m.cfgEdit.path, 1)
		if name == "" {
			m.cfgEdit.err = "$EDITOR is unset and no vi found"
			return m, nil
		}
		return m, tea.ExecProcess(exec.Command(name, args...), func(err error) tea.Msg {
			return settingsDone{err}
		})
	}
	st, closed := m.cfgEdit.update(msg)
	if !closed {
		m.cfgEdit = &st
		return m, nil
	}
	m.cfgEdit = nil
	return m.reloadConfig()
}

type settingsDone struct{ err error }

// reloadConfig перечитывает настройки проекта и пересканирует дерево.
func (m tuiModel) reloadConfig() (tea.Model, tea.Cmd) {
	cfg, err := config.Find(m.root)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.opts.cfg = cfg
	if cfg.Comments.MaxLines != nil {
		m.opts.maxLines = *cfg.Comments.MaxLines
	}
	if cfg.Comments.MaxLine != nil {
		m.opts.maxLine, m.maxLine = *cfg.Comments.MaxLine, *cfg.Comments.MaxLine
	}
	if cfg.Comments.SkipTests != nil {
		m.opts.skipTests = *cfg.Comments.SkipTests
	}
	_ = m.rescan()
	m.status = fmt.Sprintf("settings reloaded, %d findings", len(m.items))
	return m, nil
}

func (m tuiModel) itemByKey(key string) (code.Item, bool) {
	for _, it := range m.items {
		if mark(it) == key {
			return it, true
		}
	}
	return code.Item{}, false
}

// renderedLines: сколько строк займёт ответ после вставки. Предел из настроек
// про строки в файле, а не про переводы строки в ответе.
func (m tuiModel) renderedLines(it code.Item, body string) int {
	first := strings.SplitN(it.Raw, "\n", 2)[0]
	head := strings.TrimLeft(first, " \t")
	indent := first[:len(first)-len(head)]
	marker := "//"
	switch {
	case strings.HasPrefix(head, "/**"), strings.HasPrefix(head, "/*"):
		marker = " * "
	case strings.HasPrefix(head, "///"):
		marker = "///"
	case strings.HasPrefix(head, "#"):
		marker = "#"
	}
	n := 0
	for _, part := range strings.Split(body, "\n") {
		n += len(wrap(part, lineWidth(m.maxLine, len(indent)+len(marker)+1)))
	}
	return n
}

// retryRewrite просит переписать ещё раз, назвав промах по строкам. Один раз:
// дальше решает человек.
func (m tuiModel) retryRewrite(it code.Item, prev string, got int) tea.Cmd {
	req := m.rewriteRequest(it)
	req.Note = fmt.Sprintf("Прошлый ответ занял %d строк при пределе %d. Вот он:\n%s\nСократи до предела, факты сохрани.",
		got, m.opts.maxLines, prev)
	client := m.llm
	key := mark(it)
	return func() tea.Msg {
		text, use, err := client.Rewrite(context.Background(), req)
		return suggestMsg{key: key, text: text, usage: use, retry: true, err: err}
	}
}

// askModels тянет список моделей эндпоинта и открывает выбор поверх панелей.
func (m tuiModel) askModels() (tea.Model, tea.Cmd) {
	if m.llm == nil {
		m.status = "no endpoint: " + m.llmErr
		return m, nil
	}
	if m.busy != "" {
		return m, nil
	}
	client := m.llm
	m.busy = "loading models..."
	m.status = m.busy
	return m, func() tea.Msg {
		models, err := client.Models(context.Background())
		return modelsMsg{models: models, err: err}
	}
}

// rewriteRequest собирает всё, что модель должна видеть: текст блока, код
// вокруг него и претензии правил. Без кода модель пересказывает комментарий.
func (m tuiModel) rewriteRequest(it code.Item) llm.Request {
	req := llm.Request{
		Comment:  it.Raw,
		Code:     m.codeAround(it, 12),
		MaxLines: m.opts.maxLines,
		Lang:     "en",
	}
	// Тот же порог, по которому набор правил выбирается в CheckComment.
	if humanize.CyrillicShare(it.Raw) >= 0.3 {
		req.Lang = "ru"
	}
	for _, f := range it.Findings {
		req.Findings = append(req.Findings, f.Rule+": "+f.Fix)
	}
	return req
}

// codeAround - строки вокруг блока без номеров: это уходит модели, а не на экран.
func (m tuiModel) codeAround(it code.Item, around int) string {
	lines := m.fileLines(it.File)
	if len(lines) == 0 {
		return ""
	}
	end := max(it.End, it.Start)
	from, to := max(it.Start-around, 1), min(end+around, len(lines))
	return strings.Join(lines[from-1:to], "\n")
}

func (m tuiModel) openEditor() (tea.Model, tea.Cmd) {
	it, ok := m.curItem()
	if !ok {
		return m, nil
	}
	name, args := editorCmd(it.File, it.Start)
	if name == "" {
		m.status = "$EDITOR is unset and no vi found"
		return m, nil
	}
	cmd := exec.Command(name, args...)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDone{err} })
}

// editorCmd: $EDITOR бывает с аргументами ("code -w"), а перехода на строку у
// редакторов разный синтаксис. Неизвестному даём только файл.
func editorCmd(file string, line int) (string, []string) {
	ed := strings.TrimSpace(os.Getenv("EDITOR"))
	if ed == "" {
		ed = strings.TrimSpace(os.Getenv("VISUAL"))
	}
	// $EDITOR не задан у большинства: в macOS и в ubuntu-образах переменной нет,
	// а редактор есть. Без запасного варианта клавиша просто ничего не делала.
	for _, try := range []string{"nvim", "vim", "vi"} {
		if ed != "" {
			break
		}
		if _, err := exec.LookPath(try); err == nil {
			ed = try
		}
	}
	if ed == "" {
		return "", nil
	}
	fields := strings.Fields(ed)
	name, args := fields[0], fields[1:]
	switch base := filepath.Base(name); {
	case strings.HasPrefix(base, "vi"), base == "nano", base == "emacs", base == "kak", base == "hx":
		args = append(args, fmt.Sprintf("+%d", line), file)
	case base == "code", base == "cursor", base == "codium", base == "windsurf":
		args = append(args, "-g", fmt.Sprintf("%s:%d", file, line))
	case base == "subl", base == "zed":
		args = append(args, fmt.Sprintf("%s:%d", file, line))
	default:
		args = append(args, file)
	}
	return name, args
}

func (m tuiModel) View() string {
	if m.picker != nil {
		return m.picker.View()
	}
	if m.cfgEdit != nil {
		return m.cfgEdit.View(m.width)
	}
	if m.note != nil {
		return m.note.View(m.width)
	}
	// Три колонки плюс рамки и отступы: 4 знака на панель.
	inner := m.width - 12
	if inner < 48 {
		inner = 48
	}
	wRules := inner * 28 / 100
	wFiles := inner * 30 / 100
	wBlocks := inner - wRules - wFiles
	h := m.height - 5
	if h < 5 {
		h = 5
	}

	// Рамка и отступы съедают 4 знака: содержимое режется по ним, иначе
	// lipgloss переносит строку и ломает счёт строк в панели.
	panes := []string{
		m.pane(paneRules, "rules", m.rulesLines(wRules-4, h), wRules, h),
		m.pane(paneFiles, "files", m.filesLines(wFiles-4, h), wFiles, h),
		m.pane(paneBlocks, "blocks", m.blocksLines(wBlocks-4, h), wBlocks, h),
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, panes...)

	help := "tab pane  j/k move  d delete  space keep  a rewrite  y accept  n discard  m model  t todo  c settings  e editor  r rescan  q quit"
	status := m.status
	if status == "" {
		status = fmt.Sprintf("%d findings, %d done", len(m.items), len(m.done))
	}
	return m.header() + "\n" + body + "\n" + tuiFaint.Render(trimTo(help, m.width)) + "\n" + trimTo(status, m.width)
}

// header - строка о модели и расходе токенов: без неё непонятно, куда уходит
// текст и во что это обходится.
func (m tuiModel) header() string {
	model := "no model"
	if m.llm != nil && m.llm.Model() != "" {
		model = m.llm.Model()
	}
	line := "model: " + model + "   [m] change"
	if m.calls > 0 {
		line += fmt.Sprintf("   tokens: %s in / %s out   calls: %d",
			short(m.usage.In), short(m.usage.Out), m.calls)
	}
	return tuiTitle.Render(trimTo(line, m.width))
}

// short: 12400 -> 12.4k. Точные числа тут не нужны, нужен порядок.
func short(n int64) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func (m tuiModel) pane(id int, title string, lines []string, w, h int) string {
	st := tuiIdle
	if m.focus == id {
		st = tuiActive
	}
	body := tuiPane.Render(title) + "\n" + strings.Join(lines, "\n")
	return st.Width(w).Height(h).Render(body)
}

func (m tuiModel) rulesLines(w, h int) []string {
	rows := make([]string, 0, len(m.rules))
	for _, r := range m.rules {
		rows = append(rows, fmt.Sprintf("%4d  %s", r.n, r.name))
	}
	return window(rows, m.ruleIdx, h-1, w, m.focus == paneRules)
}

func (m tuiModel) filesLines(w, h int) []string {
	files := m.curFiles()
	rows := make([]string, 0, len(files))
	for _, f := range files {
		// Путь режется слева: у двух index.ts из разных плагинов различим
		// только хвост, а имени файла для выбора мало.
		rows = append(rows, fmt.Sprintf("%3d  %s", len(f.items), tailTo(m.rel(f.path), w-5)))
	}
	return window(rows, m.fileIdx, h-1, w, m.focus == paneFiles)
}

func (m tuiModel) blocksLines(w, h int) []string {
	items := m.curItems()
	var rows []string
	cursorRow := 0
	for i, it := range items {
		head := fmt.Sprintf("%d:", it.Start)
		first := strings.SplitN(strings.TrimSpace(it.Raw), "\n", 2)[0]
		row := head + " " + first
		switch {
		case m.done[mark(it)]:
			row = tuiKeepOn.Render("done ") + tuiFaint.Render(head+" "+first)
		case m.noted[mark(it)]:
			row = tuiKeepOn.Render("todo ") + head + " " + first
		}
		if i == m.blockIdx {
			// Раскрытый блок отбивается пустой строкой сверху и снизу: иначе
			// код вокруг него сливается с соседними блоками списка.
			if len(rows) > 0 {
				rows = append(rows, "")
			}
			cursorRow = len(rows)
			if m.focus == paneBlocks {
				row = tuiCursor.Render(trimTo(row, w))
			}
			rows = append(rows, trimTo(row, w))
			rows = append(rows, m.blockDetail(it, w)...)
			if i < len(items)-1 {
				rows = append(rows, "")
			}
			continue
		}
		rows = append(rows, trimTo(row, w))
	}
	return window(rows, cursorRow, h-1, w, false)
}

// blockDetail показывает блок в обрамлении кода: без соседних строк непонятно,
// что комментарий описывает, и решать по нему нечего.
func (m tuiModel) blockDetail(it code.Item, w int) []string {
	const around = 3
	lines := m.fileLines(it.File)
	// End < 0 ставится удалением: код вокруг показываем, подсвечивать нечего.
	end := it.End
	if end <= 0 {
		end = it.Start
	}
	from, to := it.Start-around, end+around
	if from < 1 {
		from = 1
	}
	if to > len(lines) {
		to = len(lines)
	}

	var out []string
	if len(lines) == 0 {
		// Файл не прочитался - показываем хотя бы сам комментарий.
		for _, l := range strings.Split(strings.TrimRight(it.Raw, "\n"), "\n") {
			out = append(out, trimTo("    │ "+l, w))
		}
	}
	for n := from; n <= to; n++ {
		body := expandTabs(lines[n-1])
		if it.End >= 0 && n >= it.Start && n <= end {
			out = append(out, trimTo(fmt.Sprintf("  ▸ %4d %s", n, body), w))
			continue
		}
		out = append(out, tuiFaint.Render(trimTo(fmt.Sprintf("    %4d %s", n, body), w)))
	}
	for _, f := range it.Findings {
		out = append(out, trimTo(tuiFaint.Render("    → "+f.Rule+": "+f.Fix), w))
	}
	if sug := m.suggest[mark(it)]; sug != "" {
		out = append(out, trimTo(tuiKeepOn.Render("    suggested (y accept, n discard):"), w))
		for _, l := range strings.Split(sug, "\n") {
			out = append(out, trimTo("    + "+l, w))
		}
	}
	return out
}

// fileLines держит прочитанные файлы до следующего скана: курсор ходит по
// блокам одного файла, читать его на каждый кадр незачем.
func (m tuiModel) fileLines(path string) []string {
	if l, ok := m.src[path]; ok {
		return l
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		m.src[path] = nil
		return nil
	}
	l := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	m.src[path] = l
	return l
}

// expandTabs: таб в панели ломает ширину, считать его за один знак нельзя.
func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// window вырезает видимый кусок списка вокруг курсора и подсвечивает его.
func window(rows []string, cur, h, w int, focused bool) []string {
	if h < 1 {
		h = 1
	}
	off := 0
	if cur >= h {
		off = cur - h + 1
	}
	end := min(off+h, len(rows))
	out := make([]string, 0, h)
	for i := off; i < end; i++ {
		row := trimTo(rows[i], w)
		if i == cur && focused {
			row = tuiCursor.Render(row)
		}
		out = append(out, row)
	}
	return out
}

// rel - путь от корня прогона: абсолютные пути в панели не помещаются.
func (m tuiModel) rel(path string) string {
	if r, err := filepath.Rel(m.root, path); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return path
}

// tailTo оставляет хвост строки: начало пути одинаковое у всех, различие в конце.
func tailTo(s string, w int) string {
	if w < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return "..." + string(r[len(r)-w+3:])
}

func trimTo(s string, w int) string {
	if w < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w])
}
