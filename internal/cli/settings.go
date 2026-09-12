package cli

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/haiodo/hum1izer/internal/config"
)

// settings - экран настроек проекта поверх панелей. Меняются числа и флаги;
// всё остальное (исключения, отключённые правила) открывается в $EDITOR.
type settings struct {
	path      string // .hum1izer.yaml, пусто если его нет
	maxLines  int
	maxLine   int
	commits   int
	skipTests bool
	langs     map[string]bool // пустая карта означает "все языки"
	idx       int
	err       string
	saved     bool
}

// settingRows: порядок строк на экране. Языки идут после общих настроек.
func (s settings) rows() []string {
	return append([]string{"max_lines", "max_line", "commits", "skip_tests"}, config.Langs()...)
}

func newSettings(cfg config.Config, o codeOpts) settings {
	s := settings{path: cfg.Path, maxLines: o.maxLines, maxLine: o.maxLine,
		commits: o.commits, skipTests: o.skipTests, langs: map[string]bool{}}
	for _, l := range cfg.Languages.Only {
		s.langs[l] = true
	}
	return s
}

// on: сканируется ли язык. Пустой список в настройках означает все языки, и на
// экране они тогда все отмечены.
func (s settings) on(lang string) bool {
	if len(s.langs) == 0 {
		return true
	}
	return s.langs[lang]
}

func (s settings) update(msg tea.KeyMsg) (settings, bool) {
	rows := s.rows()
	switch msg.String() {
	case "esc", "q", "c":
		return s, true
	case "up", "k":
		s.idx = clampIdx(s.idx-1, len(rows))
	case "down", "j":
		s.idx = clampIdx(s.idx+1, len(rows))
	case "left", "h", "-":
		s = s.bump(-1)
	case "right", "l", "+", "=":
		s = s.bump(1)
	case " ", "enter":
		s = s.toggle()
	case "s":
		s = s.save()
	}
	return s, false
}

func (s settings) bump(d int) settings {
	switch s.rows()[s.idx] {
	case "max_lines":
		s.maxLines = max(s.maxLines+d, 0)
	case "max_line":
		s.maxLine = max(s.maxLine+d*10, 0)
	case "commits":
		s.commits = max(s.commits+d, 0)
	default:
		s = s.toggle()
	}
	s.saved = false
	return s
}

func (s settings) toggle() settings {
	row := s.rows()[s.idx]
	if row == "skip_tests" {
		s.skipTests = !s.skipTests
		s.saved = false
		return s
	}
	if !config.KnownLang(row) {
		return s
	}
	// Первое снятие галочки разворачивает "все языки" в явный список: иначе
	// пустой список так и остался бы пустым и ничего не отфильтровал.
	if len(s.langs) == 0 {
		for _, l := range config.Langs() {
			s.langs[l] = true
		}
	}
	s.langs[row] = !s.langs[row]
	s.saved = false
	return s
}

func (s settings) save() settings {
	if s.path == "" {
		s.err = "no .hum1izer.yaml here, run: hum1izer init"
		return s
	}
	var only []string
	for _, l := range config.Langs() {
		if s.langs[l] {
			only = append(only, l)
		}
	}
	if len(only) == len(config.Langs()) {
		only = nil // все языки - это пустой список, а не перечисление
	}
	err := config.Patch(s.path, map[string]any{
		"comments.max_lines":  s.maxLines,
		"comments.max_line":   s.maxLine,
		"comments.commits":    s.commits,
		"comments.skip_tests": s.skipTests,
		"languages.only":      only,
	})
	if err != nil {
		s.err = err.Error()
		return s
	}
	s.err, s.saved = "", true
	return s
}

func (s settings) View(width int) string {
	var b strings.Builder
	b.WriteString(tuiTitle.Render(" settings ") + "  " + tuiFaint.Render(s.path) + "\n\n")
	for i, row := range s.rows() {
		var val string
		switch row {
		case "max_lines":
			val = fmt.Sprint(s.maxLines)
		case "max_line":
			val = fmt.Sprint(s.maxLine)
		case "commits":
			val = fmt.Sprint(s.commits)
		case "skip_tests":
			val = check(s.skipTests)
		default:
			val = check(s.on(row))
			row = "scan " + row
		}
		if i == 4 {
			b.WriteString("\n")
		}
		line := fmt.Sprintf("  %-14s %s", row, val)
		if i == s.idx {
			line = tuiCursor.Render(fmt.Sprintf("> %-14s %s", row, val))
		}
		b.WriteString(trimTo(line, width) + "\n")
	}
	switch {
	case s.err != "":
		b.WriteString("\n" + s.err + "\n")
	case s.saved:
		b.WriteString("\n" + tuiKeepOn.Render("saved") + "\n")
	default:
		b.WriteString("\n")
	}
	b.WriteString(tuiFaint.Render("j/k move   -/+ change   space toggle   s save   e edit file   esc close"))
	return b.String()
}

func check(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}
