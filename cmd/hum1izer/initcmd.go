package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/config"
	"github.com/haiodo/hum1izer/internal/humanize"
)

const initUsage = `hum1izer init - создать .hum1izer.yaml с настройками проекта.

  hum1izer init [путь]     просканировать и записать настройки (по умолчанию .)
  hum1izer init --print    показать, что получилось бы, и ничего не писать
  hum1izer init --force    перезаписать существующий файл

Сканирование нужно, чтобы вписать в файл найденные языки и подсказать, какие
правила шумят в этом проекте больше всего.
`

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, initUsage) }
	force := fs.Bool("force", false, "перезаписать существующий файл")
	printOnly := fs.Bool("print", false, "вывести в stdout, ничего не записывая")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	out := filepath.Join(root, config.Name)
	if _, err := os.Stat(out); err == nil && !*printOnly && !*force {
		fmt.Fprintf(os.Stderr, "%s уже есть, --force перезапишет\n", out)
		return 2
	}

	st, err := survey(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", root, err)
		return 2
	}
	body := render(st)
	if *printOnly {
		fmt.Print(body)
		return 0
	}
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	fmt.Printf("%s записан: языков %d, файлов %d, комментариев %d, находок %d\n",
		out, len(st.langs), st.files, st.comments, st.findings)
	return 0
}

type stats struct {
	langs                     map[string]int
	rules                     map[string]int
	files, comments, findings int
}

// survey гоняет обычную проверку на дефолтных настройках: нужны только
// количества, поэтому отчёт никуда не печатается.
func survey(root string) (stats, error) {
	st := stats{langs: map[string]int{}, rules: map[string]int{}}
	cs := code.CodeSets{MaxLines: 2}
	var err error
	if cs.RU, err = humanize.LoadBuiltin("ru"); err != nil {
		return st, err
	}
	if cs.EN, err = humanize.LoadBuiltin("en"); err != nil {
		return st, err
	}
	if cs.Code, err = humanize.LoadBuiltin("code"); err != nil {
		return st, err
	}

	paths, err := code.WalkCode(root, code.Filter{})
	if err != nil {
		return st, err
	}
	for _, p := range paths {
		st.files++
		st.langs[config.LangOf(p)]++
		cmts, err := code.ExtractComments(p)
		if err != nil {
			continue
		}
		for _, c := range cmts {
			if strings.TrimSpace(c.Text) == "" {
				continue
			}
			st.comments++
			for _, f := range code.CheckComment(cs, c, "code") {
				st.findings++
				st.rules[f.Rule]++
			}
		}
	}
	return st, nil
}

func render(st stats) string {
	var b strings.Builder
	b.WriteString(`# Настройки hum1izer для этого проекта.
# Файл ищется от проверяемого каталога вверх до корня, флаги его перекрывают.

version: 1

comments:
  # Комментарий длиннее скольких строк считать находкой. 0 отключает проверку.
  max_lines: 2
  # Сколько последних коммитов проверять, когда сканируется git-репозиторий.
  commits: 20
  skip_tests: false

languages:
`)
	fmt.Fprintf(&b, "  # Найдено в проекте: %s\n", langSummary(st.langs))
	b.WriteString(`  # only - белый список, пусто означает все поддерживаемые.
  only: []
  ignore: []

# Пути, которые не сканировать. ** проходит через каталоги, * внутри сегмента.
# node_modules, vendor, dist, gen и сгенерированные файлы пропускаются и так.
exclude: []

rules:
  # only - белый список, пусто означает все. Имя берётся из отчёта: подходит
  # и имя правила, и имя категории целиком.
  only: []
  disable: []
`)
	if top := topRules(st.rules); len(top) > 0 {
		b.WriteString("\n# Чаще всего в этом проекте срабатывает:\n")
		for _, r := range top {
			fmt.Fprintf(&b, "#   %4d  %s\n", r.n, r.name)
		}
		b.WriteString("# Если что-то из этого для проекта норма, перенеси имя в rules.disable.\n")
	}
	return b.String()
}

func langSummary(langs map[string]int) string {
	if len(langs) == 0 {
		return "ничего не найдено"
	}
	keys := make([]string, 0, len(langs))
	for l := range langs {
		keys = append(keys, l)
	}
	sort.Slice(keys, func(i, j int) bool { return langs[keys[i]] > langs[keys[j]] })
	parts := make([]string, 0, len(keys))
	for _, l := range keys {
		parts = append(parts, fmt.Sprintf("%s (%d)", l, langs[l]))
	}
	return strings.Join(parts, ", ")
}

type ruleCount struct {
	name string
	n    int
}

func topRules(m map[string]int) []ruleCount {
	out := make([]ruleCount, 0, len(m))
	for name, n := range m {
		out = append(out, ruleCount{name, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].name < out[j].name
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}
