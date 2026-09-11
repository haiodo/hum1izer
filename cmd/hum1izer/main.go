// Command hum1izer проверяет текст, комментарии в коде и сообщения коммитов
// на следы нейросети, канцелярит и штампы.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/humanize"
	"github.com/haiodo/hum1izer/internal/skill"

	"flag"
)

// version подставляется линкером при сборке релиза.
var version = "dev"

const usage = `hum1izer - проверка текста, комментариев в коде и коммитов на следы
нейросети, канцелярит и штампы.

  hum1izer [флаги] файл...         проза: md, txt, из stdin через -
  hum1izer --code путь...          комментарии в .go .ts .tsx .js .svelte .swift
  hum1izer --code репозиторий      то же плюс последние коммиты, если там git
  hum1izer install --claude ...    поставить скилл для агента

Флаги прозы:
  --genre   жанр текста: marketing (по умолчанию), academic, legal, fiction, news
  --lang    язык набора правил: ru (по умолчанию) или en
  --rules   свой файл правил вместо встроенного

Флаги кода:
  --code        разбирать аргументы как исходники, а не как прозу
  --commits N   сколько последних коммитов проверить, 0 - не проверять (20)
  --max-lines N комментарий длиннее скольких строк - находка, 0 - не считать (2)
  --skip-tests  пропускать _test.go, *.test.*, *.spec.*

Вывод:
  --format  text (по умолчанию), jsonl, json, quiet
  --limit N сколько блоков отдать, 0 - все. Первыми идут самые грязные
  --top N   сколько строк показать в сводке, 0 - все (по умолчанию 12)
  --json    то же, что --format json
  --quiet   то же, что --format quiet

jsonl - формат для агента: одна строка JSON на комментарий, внутри исходный
текст в поле raw и все находки по нему. Агент правит блок целиком, заменяет raw
точным совпадением и запускает проверку снова, пока remaining не станет нулём.

Код возврата: 0 чисто, 1 есть жёсткие находки, 2 ошибка.
`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "install" {
		os.Exit(skill.Run(os.Args[2:], version))
	}

	genre := flag.String("genre", "marketing", "жанр текста")
	lang := flag.String("lang", "ru", "язык набора правил: ru или en")
	format := flag.String("format", "text", "формат вывода: text, jsonl, json, quiet")
	asJSON := flag.Bool("json", false, "то же, что --format json")
	quiet := flag.Bool("quiet", false, "то же, что --format quiet")
	top := flag.Int("top", 12, "сколько строк показать в сводке")
	limit := flag.Int("limit", 0, "сколько блоков отдать, 0 - все")
	rulesPath := flag.String("rules", "", "свой файл правил")
	codeMode := flag.Bool("code", false, "разбирать аргументы как исходники")
	commits := flag.Int("commits", 20, "сколько последних коммитов проверить")
	maxLines := flag.Int("max-lines", 2, "комментарий длиннее скольких строк считать находкой, 0 - не считать")
	skipTests := flag.Bool("skip-tests", false, "пропускать тестовые файлы")
	showVersion := flag.Bool("version", false, "показать версию")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if *showVersion {
		fmt.Println("hum1izer " + version)
		return
	}
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	switch {
	case *asJSON:
		*format = "json"
	case *quiet:
		*format = "quiet"
	}
	if *codeMode {
		os.Exit(runCode(flag.Args(), codeOpts{
			rules: *rulesPath, format: *format, top: *top, limit: *limit,
			commits: *commits, maxLines: *maxLines, skipTests: *skipTests,
		}))
	}
	os.Exit(runText(flag.Args(), *rulesPath, *lang, *genre, *format, *top))
}

func loadProse(rulesPath, lang string) (*humanize.RuleSet, error) {
	if rulesPath != "" {
		return humanize.LoadRules(rulesPath)
	}
	return humanize.LoadBuiltin(lang)
}

func runText(args []string, rulesPath, lang, genre, format string, top int) int {
	rs, err := loadProse(rulesPath, lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "правила: %v\n", err)
		return 2
	}
	if !rs.HasGenre(genre) {
		fmt.Fprintf(os.Stderr, "неизвестный жанр %q, доступны: %s\n", genre, strings.Join(rs.Genres, ", "))
		return 2
	}

	exit := 0
	for _, src := range args {
		text, err := read(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", src, err)
			exit = 2
			continue
		}
		if strings.TrimSpace(text) == "" {
			fmt.Printf("%s: текст пуст, сканировать нечего\n", src)
			continue
		}
		rep := humanize.Analyze(rs, text, genre)
		switch format {
		case "json", "jsonl":
			printJSON(src, rep)
		case "quiet":
			fmt.Printf("%-40s %3d/100 [%s] банов: %d, маркеров: %d\n",
				src, rep.Score.Value, rep.Score.Band, countHits(rep.HardBans), countHits(rep.Markers))
		default:
			printReport(rs, src, rep, top)
		}
		if len(rep.HardBans) > 0 && exit == 0 {
			exit = 1
		}
	}
	return exit
}

type codeOpts struct {
	rules, format       string
	top, limit, commits int
	maxLines            int
	skipTests           bool
}

// runCode: комментарии из исходников и, если в аргументе лежит git-репозиторий,
// сообщения последних коммитов. И то и другое проверяется как обычная проза.
func runCode(args []string, o codeOpts) int {
	cs := code.CodeSets{MaxLines: o.maxLines}
	var err error
	if cs.RU, err = loadProse(o.rules, "ru"); err != nil {
		fmt.Fprintf(os.Stderr, "русские правила: %v\n", err)
		return 2
	}
	if cs.EN, err = humanize.LoadBuiltin("en"); err != nil {
		fmt.Fprintf(os.Stderr, "английские правила: %v\n", err)
		return 2
	}
	if cs.Code, err = humanize.LoadBuiltin("code"); err != nil {
		fmt.Fprintf(os.Stderr, "правила для кода: %v\n", err)
		return 2
	}

	var items []code.Item
	files, blocks, exit := 0, 0, 0
	collect := func(c code.Comment, genre string) {
		blocks++
		if f := code.CheckComment(cs, c, genre); len(f) > 0 {
			items = append(items, code.NewItem(c, f))
		}
	}

	for _, root := range args {
		paths, err := code.WalkCode(root, o.skipTests)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", root, err)
			exit = 2
			continue
		}
		for _, p := range paths {
			cmts, err := code.ExtractComments(p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
				continue
			}
			files++
			for _, c := range cmts {
				if strings.TrimSpace(c.Text) == "" {
					continue
				}
				collect(c, "code")
			}
		}
		if o.commits > 0 {
			cmts, err := code.GitCommits(root, o.commits)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", root, err)
			}
			for _, c := range cmts {
				collect(c, "commit")
			}
		}
	}

	code.SortItems(items)
	for _, it := range items {
		for _, f := range it.Findings {
			if f.Hard {
				exit = 1
			}
		}
	}
	switch o.format {
	case "jsonl":
		printJSONL(items, o.limit, files, blocks)
	case "json":
		printFindingsJSON(items, o.limit)
	case "quiet":
		fmt.Printf("файлов: %d, комментариев: %d, блоков с находками: %d, находок: %d\n",
			files, blocks, len(items), countFindings(items))
	default:
		printCodeReport(items, files, blocks, o.limit, o.top)
	}
	return exit
}

func read(src string) (string, error) {
	if src == "-" {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	b, err := os.ReadFile(src)
	return string(b), err
}
