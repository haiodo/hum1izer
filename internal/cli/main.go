// Command hum1izer проверяет текст, комментарии в коде и сообщения коммитов
// на следы нейросети, канцелярит и штампы.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/haiodo/hum1izer/internal/baseline"
	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/config"
	"github.com/haiodo/hum1izer/internal/humanize"
	"github.com/haiodo/hum1izer/internal/skill"

	"flag"
)

// Version подставляется линкером при сборке релиза.
var Version = "dev"

const usage = `hum1izer - проверка текста, комментариев в коде и коммитов на следы
нейросети, канцелярит и штампы.

  hum1izer [флаги] файл...         проза: md, txt, из stdin через -
  hum1izer --code путь...          комментарии в .go .ts .js .svelte .swift .java .kt
  hum1izer --code репозиторий      то же плюс последние коммиты, если там git
  hum1izer install --claude ...    поставить скилл для агента
  hum1izer init [путь]             создать .hum1izer.yaml с настройками проекта
  hum1izer upgrade                 обновиться до последнего релиза с GitHub
  hum1izer fix --block <хэш> ...   удалить или заменить блок комментария

Флаги прозы:
  --genre   жанр текста: marketing (по умолчанию), academic, legal, fiction, news
  --lang    язык набора правил: ru (по умолчанию) или en
  --rules   свой файл правил вместо встроенного

Флаги кода:
  --code        разбирать аргументы как исходники, а не как прозу
  --commits N   сколько последних коммитов проверить, 0 - не проверять (20)
  --max-lines N комментарий длиннее скольких строк - находка, 0 - не считать (2)
  --max-line N  строка комментария длиннее скольких символов - находка (100)
  --skip-tests  пропускать _test.go, *.test.*, *.spec.*

Настройки проекта:
  .hum1izer.yaml ищется от проверяемого каталога вверх до корня. В нём живут
  предел длины комментария, языки, исключения и отключённые правила. Флаги
  командной строки перекрывают файл.
  --config F  взять этот файл настроек
  --no-config игнорировать .hum1izer.yaml

Снимок для CI:
  --baseline F      сверяться с файлом снимка и ругаться только на новое
  --write-baseline  перезаписать снимок текущими находками

Вывод:
  --format  text (по умолчанию), md, jsonl, json, quiet
  --limit N сколько блоков отдать, 0 - все. Первыми идут самые грязные
  --top N   сколько строк показать в сводке, 0 - все (по умолчанию 12)
  --json    то же, что --format json
  --quiet   то же, что --format quiet

md - формат для агента: заголовок на комментарий, список замечаний и сам текст
в огороженном блоке, байт в байт. Агент правит блок целиком, заменяет текст
точным совпадением и запускает проверку снова, пока remaining не станет нулём.
jsonl - то же самое машинно, по строке JSON на комментарий. Текст там
экранирован, поэтому для точной замены он менее удобен.

Код возврата: 0 чисто, 1 есть жёсткие находки, 2 ошибка.
`

// Run - точка входа. Вынесена из main, чтобы модуль давал рабочую команду и по
// короткому пути (go run github.com/haiodo/hum1izer@latest), и по длинному.
func Run() int {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			return skill.Run(os.Args[2:], Version)
		case "init":
			return runInit(os.Args[2:])
		case "upgrade":
			return runUpgrade(os.Args[2:])
		case "fix":
			return runFix(os.Args[2:])
		}
	}

	genre := flag.String("genre", "marketing", "жанр текста")
	lang := flag.String("lang", "ru", "язык набора правил: ru или en")
	format := flag.String("format", "text", "формат вывода: text, md, jsonl, json, quiet")
	asJSON := flag.Bool("json", false, "то же, что --format json")
	quiet := flag.Bool("quiet", false, "то же, что --format quiet")
	top := flag.Int("top", 12, "сколько строк показать в сводке")
	limit := flag.Int("limit", 0, "сколько блоков отдать, 0 - все")
	rulesPath := flag.String("rules", "", "свой файл правил")
	codeMode := flag.Bool("code", false, "разбирать аргументы как исходники")
	commits := flag.Int("commits", 20, "сколько последних коммитов проверить")
	maxLines := flag.Int("max-lines", 2, "комментарий длиннее скольких строк считать находкой, 0 - не считать")
	maxLine := flag.Int("max-line", 100, "строка комментария длиннее скольких символов - находка, 0 - не считать")
	skipTests := flag.Bool("skip-tests", false, "пропускать тестовые файлы")
	basePath := flag.String("baseline", "", "файл снимка: ругаться только на новое")
	writeBase := flag.Bool("write-baseline", false, "перезаписать снимок текущими находками")
	cfgPath := flag.String("config", "", "файл настроек вместо поиска .hum1izer.yaml")
	noConfig := flag.Bool("no-config", false, "игнорировать .hum1izer.yaml")
	showVersion := flag.Bool("version", false, "показать версию")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if *showVersion {
		fmt.Println("hum1izer " + Version)
		return 0
	}
	if flag.NArg() == 0 {
		flag.Usage()
		return 2
	}
	given := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { given[f.Name] = true })

	cfg, err := loadConfig(*cfgPath, *noConfig, flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "настройки: %v\n", err)
		return 2
	}
	if !given["rules"] && cfg.RulesFile != "" {
		*rulesPath = resolve(cfg, cfg.RulesFile)
	}
	if !given["genre"] && cfg.Genre != "" {
		*genre = cfg.Genre
	}
	if !given["max-lines"] && cfg.Comments.MaxLines != nil {
		*maxLines = *cfg.Comments.MaxLines
	}
	if !given["max-line"] && cfg.Comments.MaxLine != nil {
		*maxLine = *cfg.Comments.MaxLine
	}
	if !given["commits"] && cfg.Comments.Commits != nil {
		*commits = *cfg.Comments.Commits
	}
	if !given["skip-tests"] && cfg.Comments.SkipTests != nil {
		*skipTests = *cfg.Comments.SkipTests
	}
	if !given["baseline"] && cfg.Baseline != "" {
		*basePath = resolve(cfg, cfg.Baseline)
	}
	// Снимок коммитов бессмыслен: каждый коммит - новая находка, файл пришлось бы
	// переписывать после каждого. Сообщения проверяет отдельный прогон или хук commit-msg.
	if *basePath != "" && !given["commits"] {
		*commits = 0
	}
	if *writeBase && *basePath == "" {
		fmt.Fprintln(os.Stderr, "--write-baseline без --baseline ничего не пишет: укажи файл снимка")
	}
	switch {
	case *asJSON:
		*format = "json"
	case *quiet:
		*format = "quiet"
	}
	if *codeMode {
		return runCode(flag.Args(), codeOpts{
			cfg: cfg, rules: *rulesPath, format: *format, top: *top, limit: *limit,
			commits: *commits, maxLines: *maxLines, maxLine: *maxLine, skipTests: *skipTests,
			baseline: *basePath, writeBaseline: *writeBase,
		})
	}
	return runText(flag.Args(), *rulesPath, *lang, *genre, *format, *top)
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
		if isMarkup(src) {
			text = humanize.StripMarkup(text)
		}
		rep := humanize.Analyze(rs, text, genre)
		switch format {
		case "md":
			fmt.Fprintln(os.Stderr, "--format md есть только у --code, для прозы доступны text, json, quiet")
			return 2
		case "json", "jsonl":
			printJSON(src, rep)
		case "quiet":
			phrases, dashes := splitBans(rep.HardBans)
			fmt.Printf("%-40s %3d/100 [%s] банов: %d (+%d тире), маркеров: %.1f/100 слов\n",
				src, rep.Score.Value, rep.Score.Band, phrases, dashes,
				per100(countHits(rep.Markers), rep.Rhythm.Words))
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
	cfg                 config.Config
	rules, format       string
	baseline            string
	top, limit, commits int
	maxLines, maxLine   int
	skipTests           bool
	writeBaseline       bool
}

// loadConfig: явный файл, поиск вверх от цели проверки, либо пустые настройки.
func loadConfig(path string, disabled bool, target string) (config.Config, error) {
	switch {
	case disabled:
		return config.Config{}, nil
	case path != "":
		return config.Load(path)
	case target == "-":
		return config.Find(".")
	}
	return config.Find(target)
}

// resolve: путь из настроек считается от каталога с файлом настроек.
func resolve(cfg config.Config, path string) string {
	if cfg.Path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(filepath.Dir(cfg.Path), path)
}

// runCode: комментарии из исходников и, если в аргументе лежит git-репозиторий,
// сообщения последних коммитов. И то и другое проверяется как обычная проза.
func runCode(args []string, o codeOpts) int {
	cs := code.CodeSets{MaxLines: o.maxLines, MaxLineLen: o.maxLine}
	excludes, err := o.cfg.Excludes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "исключения: %v\n", err)
		return 2
	}
	filter := code.Filter{SkipTests: o.skipTests, Langs: o.cfg.AllowedLangs(), Exclude: excludes}

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
		if f := allowed(o.cfg, code.CheckComment(cs, c, genre)); len(f) > 0 {
			items = append(items, code.NewItem(c, f))
		}
	}

	for _, root := range args {
		paths, err := code.WalkCode(root, filter)
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

	if o.baseline != "" {
		var err error
		if items, exit, err = applyBaseline(o, items); err != nil {
			fmt.Fprintf(os.Stderr, "снимок: %v\n", err)
			return 2
		}
		if o.writeBaseline {
			return exit
		}
	} else {
		for _, it := range items {
			for _, f := range it.Findings {
				if f.Hard {
					exit = 1
				}
			}
		}
	}
	switch o.format {
	case "md":
		printMarkdown(items, o.limit, files, blocks)
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

// applyBaseline оставляет только новые находки. Код возврата 1 - за новое, а не
// за жёсткое: с тысячей существующих находок иначе не встроиться в CI.
func applyBaseline(o codeOpts, items []code.Item) ([]code.Item, int, error) {
	if o.writeBaseline {
		var all []baseline.Entry
		for _, it := range items {
			for _, f := range it.Findings {
				all = append(all, baseline.Entry{Hash: it.Hash, Rule: f.Rule})
			}
		}
		if err := baseline.Write(o.baseline, all); err != nil {
			return nil, 0, err
		}
		fmt.Printf("снимок записан: %s, находок %d\n", o.baseline, len(all))
		return nil, 0, nil
	}

	base, err := baseline.Load(o.baseline)
	if err != nil {
		return nil, 0, err
	}
	fresh := items[:0:0]
	newCount := 0
	for _, it := range items {
		kept := it.Findings[:0:0]
		for _, f := range it.Findings {
			if base.Known(baseline.Entry{Hash: it.Hash, Rule: f.Rule}) {
				continue
			}
			kept = append(kept, f)
		}
		if len(kept) == 0 {
			continue
		}
		it.Findings = kept
		newCount += len(kept)
		fresh = append(fresh, it)
	}
	fmt.Fprintf(os.Stderr, "снимок %s: в базе %d, новых %d, исправлено %d\n",
		o.baseline, base.Size(), newCount, base.Fixed())
	exit := 0
	if newCount > 0 {
		exit = 1
	}
	return fresh, exit, nil
}

// allowed отсеивает то, что проект отключил в .hum1izer.yaml.
func allowed(cfg config.Config, f []code.Finding) []code.Finding {
	if len(cfg.Rules.Only) == 0 && len(cfg.Rules.Disable) == 0 {
		return f
	}
	out := f[:0:0]
	for _, x := range f {
		if cfg.Allow(x.Rule, x.Category) {
			out = append(out, x)
		}
	}
	return out
}

func per100(n, words int) float64 {
	if words == 0 {
		return 0
	}
	return float64(n) / float64(words) * 100
}

// isMarkup: у markdown и mdx гасим код и разметку. Для txt и stdin не гадаем.
func isMarkup(src string) bool {
	switch strings.ToLower(filepath.Ext(src)) {
	case ".md", ".mdx", ".markdown":
		return true
	}
	return false
}

func read(src string) (string, error) {
	if src == "-" {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	b, err := os.ReadFile(src)
	return string(b), err
}
