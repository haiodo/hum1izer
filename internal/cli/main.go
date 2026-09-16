// Command hum1izer проверяет текст, комментарии в коде и сообщения коммитов
// на следы нейросети, канцелярит и штампы.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/haiodo/hum1izer/internal/baseline"
	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/config"
	"github.com/haiodo/hum1izer/internal/humanize"
	"github.com/haiodo/hum1izer/internal/skill"

	"flag"
)

// Version подставляется линкером при сборке релиза.
var Version = "dev"

const usage = `hum1izer - checks prose, code comments and commit messages for AI
traces, officialese and cliches.

  hum1izer [flags] file...         prose: md, txt, stdin via -
  hum1izer --code path...          comments in .go .c .cpp .ts .js .svelte .swift .java .kt
  hum1izer --code repo             same, plus recent commits if it's a git repo
  hum1izer install --claude ...    install the skill for an agent
  hum1izer init [path]             create .hum1izer.yaml with project settings
  hum1izer upgrade                 upgrade to the latest GitHub release
  hum1izer fix --block <hash> ...  remove or replace a comment block
  hum1izer tui [path]              triage findings by hand in three panes
  hum1izer llm --key <key>         pick a model for rewriting
  hum1izer calibrate [path]        tune thresholds for this repo
  hum1izer hook post-edit          post-edit check, called by an agent

Prose flags:
  --genre   text genre: marketing (default), academic, legal, fiction, news
  --lang    rule set language: ru (default) or en
  --rules   custom rules file instead of the built-in one

Code flags:
  --code        treat arguments as source code, not prose
  --commits N   how many recent commits to check, 0 - skip (20)
  --max-lines N comment longer than this many lines is a finding, 0 - don't count (2)
  --max-line N  comment line longer than this many chars is a finding (100)
  --skip-tests  skip _test.go, *.test.*, *.spec.*

Project settings:
  .hum1izer.yaml is looked up from the checked directory up to the root. It
  holds the comment length limit, languages, excludes and disabled rules.
  Command-line flags override the file.
  --config F  use this settings file
  --no-config ignore .hum1izer.yaml

CI baseline:
  --baseline F      compare against a baseline file, flag only new findings
  --write-baseline  overwrite the baseline with current findings

Output:
  --format  text (default), md, jsonl, json, quiet
  --only    keep only these rules or categories, comma-separated
  --langs   only these languages: c, cpp, go, ts, js, svelte, swift, java, kotlin
  --limit N how many blocks to emit, 0 - all. Worst blocks come first
  --top N   how many lines in the summary, 0 - all (default 12)

  A directory argument gives a summary by rule, without the findings
  themselves: a tree has thousands of them. Name a file and findings print in
  full.
  --json    same as --format json
  --quiet   same as --format quiet

md - format for an agent: a header per comment, a list of notes and the text
itself in a fenced block, byte for byte. The agent edits the whole block,
replaces the text with an exact match and reruns the check until remaining
hits zero.
jsonl - the same, machine-readable, one JSON line per comment. The text is
escaped there, so it's less convenient for an exact replace.

Exit code: 0 clean, 1 hard findings, 2 error.
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
		case "tui":
			return runTUI(os.Args[2:])
		case "llm":
			return runLLMCmd(os.Args[2:])
		case "calibrate":
			return runCalibrate(os.Args[2:])
		case "hook":
			return runHook(os.Args[2:])
		}
	}

	genre := flag.String("genre", "marketing", "text genre")
	lang := flag.String("lang", "ru", "rule set language: ru or en")
	format := flag.String("format", "text", "output format: text, md, jsonl, json, quiet")
	asJSON := flag.Bool("json", false, "same as --format json")
	quiet := flag.Bool("quiet", false, "same as --format quiet")
	top := flag.Int("top", 12, "how many lines in the summary")
	limit := flag.Int("limit", 0, "how many blocks to emit, 0 - all")
	rulesPath := flag.String("rules", "", "custom rules file")
	codeMode := flag.Bool("code", false, "treat arguments as source code")
	commits := flag.Int("commits", 20, "how many recent commits to check")
	maxLines := flag.Int("max-lines", 2, "comment longer than this many lines is a finding, 0 - don't count")
	maxLine := flag.Int("max-line", 100, "comment line longer than this many chars is a finding, 0 - don't count")
	skipTests := flag.Bool("skip-tests", false, "skip test files")
	basePath := flag.String("baseline", "", "baseline file: flag only new findings")
	writeBase := flag.Bool("write-baseline", false, "overwrite the baseline with current findings")
	cfgPath := flag.String("config", "", "settings file instead of looking up .hum1izer.yaml")
	noConfig := flag.Bool("no-config", false, "ignore .hum1izer.yaml")
	only := flag.String("only", "", "keep only these rules or categories, comma-separated")
	langs := flag.String("langs", "", "scan only these languages, comma-separated")
	showVersion := flag.Bool("version", false, "show version")
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
		fmt.Fprintf(os.Stderr, "settings: %v\n", err)
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
	if *langs != "" {
		cfg.Languages.Only = splitList(*langs)
		for _, l := range cfg.Languages.Only {
			if !config.KnownLang(l) {
				fmt.Fprintf(os.Stderr, "--langs: unknown language %q, known: %s\n", l, strings.Join(config.Langs(), ", "))
				return 2
			}
		}
	}
	if *only != "" {
		cfg.Rules.Only = splitList(*only)
	}
	// Снимок коммитов бессмыслен: каждый коммит - новая находка, файл пришлось бы
	// переписывать после каждого. Сообщения проверяет отдельный прогон или хук commit-msg.
	if *basePath != "" && !given["commits"] {
		*commits = 0
	}
	if *writeBase && *basePath == "" {
		fmt.Fprintln(os.Stderr, "--write-baseline without --baseline writes nothing: give a baseline file")
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
		fmt.Fprintf(os.Stderr, "rules: %v\n", err)
		return 2
	}
	if !rs.HasGenre(genre) {
		fmt.Fprintf(os.Stderr, "unknown genre %q, available: %s\n", genre, strings.Join(rs.Genres, ", "))
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
			fmt.Printf("%s: text is empty, nothing to scan\n", src)
			continue
		}
		if isMarkup(src) {
			text = humanize.StripMarkup(text)
		}
		rep := humanize.Analyze(rs, text, genre)
		switch format {
		case "md":
			fmt.Fprintln(os.Stderr, "--format md only works with --code, prose supports text, json, quiet")
			return 2
		case "json", "jsonl":
			printJSON(src, rep)
		case "quiet":
			phrases, dashes := splitBans(rep.HardBans)
			fmt.Printf("%-40s %3d/100 [%s] bans: %d (+%d dashes), markers: %.1f/100 words\n",
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

// collectItems - общий сбор находок для отчёта и для TUI. Возвращает exit 2 и
// nil, если правила или настройки не читаются: дальше идти не с чем.
func collectItems(args []string, o codeOpts) (items []code.Item, files, blocks, exit int) {
	cs := code.CodeSets{MaxLines: o.maxLines, MaxLineLen: o.maxLine}
	excludes, err := o.cfg.Excludes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "excludes: %v\n", err)
		return nil, 0, 0, 2
	}
	filter := code.Filter{SkipTests: o.skipTests, Langs: o.cfg.AllowedLangs(), Exclude: excludes}

	if cs.RU, err = loadProse(o.rules, "ru"); err != nil {
		fmt.Fprintf(os.Stderr, "ru rules: %v\n", err)
		return nil, 0, 0, 2
	}
	if cs.EN, err = humanize.LoadBuiltin("en"); err != nil {
		fmt.Fprintf(os.Stderr, "en rules: %v\n", err)
		return nil, 0, 0, 2
	}
	if cs.Code, err = humanize.LoadBuiltin("code"); err != nil {
		fmt.Fprintf(os.Stderr, "code rules: %v\n", err)
		return nil, 0, 0, 2
	}

	for _, root := range args {
		paths, err := code.WalkCode(root, filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", root, err)
			exit = 2
			continue
		}
		scanned, found, n := scanFiles(paths, cs, o.cfg)
		files += n
		blocks += scanned
		items = append(items, found...)
		if o.commits > 0 {
			cmts, err := code.GitCommits(root, o.commits)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", root, err)
			}
			for _, c := range cmts {
				blocks++
				if f := allowed(o.cfg, code.CheckComment(cs, c, "commit")); len(f) > 0 {
					items = append(items, code.NewItem(c, f))
				}
			}
		}
	}
	code.SortItems(items)
	return items, files, blocks, exit
}

// runCode: комментарии из исходников и, если в аргументе лежит git-репозиторий,
// сообщения последних коммитов. И то и другое проверяется как обычная проза.
func runCode(args []string, o codeOpts) int {
	items, files, blocks, exit := collectItems(args, o)
	if exit == 2 && items == nil {
		return 2
	}

	if o.baseline != "" {
		var err error
		if items, exit, err = applyBaseline(o, items); err != nil {
			fmt.Fprintf(os.Stderr, "baseline: %v\n", err)
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
		fmt.Printf("files: %d, comments: %d, blocks with findings: %d, findings: %d\n",
			files, blocks, len(items), countFindings(items))
	default:
		printCodeReport(items, files, blocks, o.limit, o.top, allFiles(args))
	}
	return exit
}

// splitList - список через запятую с обрезкой пробелов и пустых элементов.
func splitList(s string) []string {
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// allFiles: прогон назвали поимённо, а не каталогом - тогда находки печатаются
// целиком. На дереве вместо них идёт сводка по правилам.
func allFiles(args []string) bool {
	for _, a := range args {
		st, err := os.Stat(a)
		if err != nil || st.IsDir() {
			return false
		}
	}
	return len(args) > 0
}

// scanFiles разбирает файлы пулом воркеров: проверка правил занимает 95% времени
// прогона и упирается в одно ядро. Порядок результата не важен, дальше SortItems.
func scanFiles(paths []string, cs code.CodeSets, cfg config.Config) (blocks int, items []code.Item, files int) {
	workers := min(runtime.NumCPU(), len(paths))
	if workers < 1 {
		workers = 1
	}
	type part struct {
		items  []code.Item
		blocks int
		files  int
	}
	parts := make([]part, workers)
	next := make(chan int, len(paths))
	for i := range paths {
		next <- i
	}
	close(next)

	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range next {
				cmts, err := code.ExtractComments(paths[i])
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s: %v\n", paths[i], err)
					continue
				}
				parts[w].files++
				for _, c := range cmts {
					if strings.TrimSpace(c.Text) == "" {
						continue
					}
					parts[w].blocks++
					if f := allowed(cfg, code.CheckComment(cs, c, "code")); len(f) > 0 {
						parts[w].items = append(parts[w].items, code.NewItem(c, f))
					}
				}
			}
		}(w)
	}
	wg.Wait()

	for _, p := range parts {
		blocks += p.blocks
		files += p.files
		items = append(items, p.items...)
	}
	return blocks, items, files
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
		fmt.Printf("baseline written: %s, findings %d\n", o.baseline, len(all))
		return nil, 0, nil
	}

	if _, err := os.Stat(o.baseline); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "baseline %s not found: all findings count as new\n", o.baseline)
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
	fmt.Fprintf(os.Stderr, "baseline %s: known %d, new %d, fixed %d\n",
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
