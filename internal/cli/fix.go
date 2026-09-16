package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/haiodo/hum1izer/internal/baseline"
	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/config"
	"github.com/haiodo/hum1izer/internal/humanize"
)

const fixUsage = `hum1izer fix - apply an edit to a comment block by the hash from the report.

  hum1izer fix --block <hash> --delete path         remove the whole block
  hum1izer fix --block <hash> --text "<text>" path  replace the block's text
  hum1izer fix --block <hash> --text - path         same, text from stdin
  hum1izer fix --block <hash> --keep path           don't edit: add to baseline
  hum1izer fix --auto path                          remove mechanical findings
  hum1izer fix --batch - path                       batch: JSONL from stdin

  --write     apply; without it only a preview prints
  --max-line  wrap long lines on replace (100)

Batch - one JSON line per edit, the tree is walked once:

  {"block":"55dc2ad977ba","text":"new comment text"}
  {"block":"a832aeaac8cf","delete":true}
  {"block":"56fa9eb0ddfa","file":"src/a.ts","delete":true}

Identical text in different files is one block with one hash. Without file
the edit hits every copy, with file - only the one named.

Looked and decided to leave it - {"block":"...","keep":true}: the block goes
into the baseline (--baseline or baseline from .hum1izer.yaml) and won't show
up on the next run. To mark it right in the code, put hum1izer:keep in the
comment text.

A block is found by hash, not by line number: after the first edit lines
shift, the hash doesn't. Text is given without // and /* */ - the tool
restores the marker, indent and wrapping itself.
`

func runFix(args []string) int {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, fixUsage) }
	block := fs.String("block", "", "block hash from the report")
	del := fs.Bool("delete", false, "remove the whole block")
	text := fs.String("text", "", "new comment text, - reads stdin")
	auto := fs.Bool("auto", false, "remove mechanical findings: commented-out code and placeholder comments")
	batch := fs.String("batch", "", "JSONL file with edits, - reads stdin")
	keep := fs.Bool("keep", false, "don't edit: add the block to the baseline as accepted")
	basePath := fs.String("baseline", "", "baseline file for --keep (default from .hum1izer.yaml)")
	write := fs.Bool("write", false, "apply the edit")
	maxLine := fs.Int("max-line", 100, "wrap long lines on replace")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}

	switch {
	case *batch != "":
		return fixBatch(root, *batch, *basePath, *write, *maxLine)
	case *auto:
		return fixAuto(root, *write)
	case *block == "":
		fs.Usage()
		return 2
	case *keep:
		blocks, err := locateAll(root, *block)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return keepBlocks(root, *basePath, blocks, *write)
	case *del && *text != "":
		fmt.Fprintln(os.Stderr, "--delete and --text together make no sense")
		return 2
	case !*del && *text == "":
		fmt.Fprintln(os.Stderr, "need --delete or --text")
		return 2
	}

	body := *text
	if body == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		body = strings.TrimSpace(string(b))
	}

	if !*del && placeholder(body) {
		fmt.Fprintln(os.Stderr, "--text: this is the report's template, fill in the real text")
		return 2
	}

	blocks, err := locateAll(root, *block)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var edits []edit
	for _, c := range blocks {
		raw, err := os.ReadFile(c.File)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		e, err := plan(c, string(raw), *del, body, *maxLine)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		edits = append(edits, e)
	}
	return apply(edits, *write)
}

type batchEdit struct {
	Block  string `json:"block"`
	Hash   string `json:"hash"`
	File   string `json:"file"`
	Text   string `json:"text"`
	Delete bool   `json:"delete"`
	Keep   bool   `json:"keep"`
}

// key - ключ правки. С file правка достанется только этому файлу, без него -
// всем копиям текста: один и тот же комментарий в трёх файлах даёт один хэш.
func (b batchEdit) key() string {
	if b.File == "" {
		return b.id()
	}
	return b.id() + "\t" + abs(b.File)
}

func abs(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

func (b batchEdit) id() string {
	if b.Block != "" {
		return b.Block
	}
	return b.Hash
}

// fixBatch применяет пачку правок за один обход дерева: на большом репозитории
// обход стоит секунды, и делать его на каждую правку незачем.
func fixBatch(root, src, basePath string, write bool, maxLine int) int {
	in := os.Stdin
	if src != "-" {
		f, err := os.Open(src)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		defer func() { _ = f.Close() }()
		in = f
	}

	want := map[string]batchEdit{}
	skipped := 0
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan(); line++ {
		t := strings.TrimSpace(sc.Text())
		if t == "" {
			continue
		}
		var b batchEdit
		if err := json.Unmarshal([]byte(t), &b); err != nil {
			fmt.Fprintf(os.Stderr, "line %d: %v\n", line, err)
			return 2
		}
		if b.id() == "" {
			fmt.Fprintf(os.Stderr, "line %d: missing block or hash field\n", line)
			return 2
		}
		if b.Text == "" && !b.Delete && !b.Keep {
			skipped++
			continue
		}
		if !b.Delete && !b.Keep && placeholder(b.Text) {
			fmt.Fprintf(os.Stderr, "line %d: text is a template, not real text\n", line)
			return 2
		}
		want[b.key()] = b
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	paths, err := walkPaths(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var edits []edit
	var kept []code.Comment
	found := map[string]bool{}
	for _, p := range paths {
		cmts, err := code.ExtractComments(p)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, c := range cmts {
			h := baseline.Hash(c.Text)
			b, ok := want[h+"\t"+abs(p)]
			if !ok {
				b, ok = want[h]
			}
			if !ok {
				continue
			}
			found[b.key()] = true
			if b.Keep {
				kept = append(kept, c)
				continue
			}
			e, err := plan(c, string(raw), b.Delete, b.Text, maxLine)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", b.id(), err)
				found[b.key()] = false
				continue
			}
			edits = append(edits, e)
		}
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "skipped without edit: %d\n", skipped)
	}
	exit := 0
	for k, b := range want {
		if !found[k] {
			fmt.Fprintf(os.Stderr, "block %s not found\n", b.id())
			exit = 2
		}
	}
	if len(kept) > 0 {
		if rc := keepBlocks(root, basePath, kept, write); rc != 0 {
			return rc
		}
	}
	if rc := apply(edits, write); rc != 0 {
		return rc
	}
	return exit
}

// edit - замена байтового диапазона [so, eo) текстом new: комментарий бывает посреди строки кода
type edit struct {
	file   string
	so, eo int
	new    string
	line   int      // строка начала, для предпросмотра
	old    []string // что уходит, для предпросмотра
}

// walkFilter - тот же отбор файлов, что и у основного прогона: правка не должна
// лезть в то, что проект исключил в .hum1izer.yaml.
func walkFilter(root string) (code.Filter, error) {
	cfg, err := config.Find(root)
	if err != nil {
		return code.Filter{}, fmt.Errorf("settings: %w", err)
	}
	excludes, err := cfg.Excludes()
	if err != nil {
		return code.Filter{}, fmt.Errorf("excludes: %w", err)
	}
	skipTests := cfg.Comments.SkipTests != nil && *cfg.Comments.SkipTests
	return code.Filter{Langs: cfg.AllowedLangs(), Exclude: excludes, SkipTests: skipTests}, nil
}

// walkPaths - файлы под правку. Настройки битые - правку не начинаем: молча
// пройтись по исключённому дереву хуже, чем не пройтись вовсе.
func walkPaths(root string) ([]string, error) {
	f, err := walkFilter(root)
	if err != nil {
		return nil, err
	}
	return code.WalkCode(root, f)
}

// locateAll - все блоки с этим хэшем: ключ находки - текст, а не файл, поэтому
// три одинаковых комментария правятся разом.
func locateAll(root, hash string) ([]code.Comment, error) {
	paths, err := walkPaths(root)
	if err != nil {
		return nil, err
	}
	var out []code.Comment
	for _, p := range paths {
		cmts, err := code.ExtractComments(p)
		if err != nil {
			continue
		}
		for _, c := range cmts {
			if baseline.Hash(c.Text) == hash {
				out = append(out, c)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("block %s not found in %s: text changed or wrong path", hash, root)
	}
	return out, nil
}

func plan(c code.Comment, src string, del bool, body string, maxLine int) (edit, error) {
	if c.SO < 0 || c.EO > len(src) || c.SO >= c.EO {
		return edit{}, fmt.Errorf("%s: block outside file, offsets %d-%d", c.File, c.SO, c.EO)
	}

	// Сканер не для Go забирает в блок и \r: он часть перевода строки, а не текста
	// комментария, и вырезать его нельзя - получатся смешанные концы строк.
	eo := c.EO
	if eo > c.SO && src[eo-1] == '\r' {
		eo--
	}
	lineStart := strings.LastIndexByte(src[:c.SO], '\n') + 1
	lineEnd := len(src)
	if i := strings.IndexByte(src[eo:], '\n'); i >= 0 {
		lineEnd = eo + i
	}
	prefix := src[lineStart:c.SO] // что на строке до комментария
	suffix := src[eo:lineEnd]     // и что после него
	ownLine := strings.TrimSpace(prefix) == ""
	e := edit{file: c.File, line: c.Start, old: strings.Split(src[lineStart:lineEnd], "\n")}

	if !del {
		if !ownLine {
			return edit{}, fmt.Errorf("replacing a trailing comment is not supported, edit it by hand")
		}
		e.so, e.eo = lineStart, eo
		head := strings.SplitN(c.Raw, "\n", 2)[0]
		e.new = strings.Join(renderComment(prefix, head, body, maxLine), nl(crlf(src)))
		return e, nil
	}

	// Строка была только под комментарий - убираем её целиком вместе с переводом
	// строки, иначе после правки остаётся пустая.
	if ownLine && strings.TrimSpace(suffix) == "" {
		e.so, e.eo = lineStart, min(lineEnd+1, len(src))
		return e, nil
	}
	// Комментарий посреди кода: снимаем только его, код слева и справа остаётся.
	e.so, e.eo = c.SO, eo
	if strings.TrimSpace(suffix) == "" {
		e.so = lineStart + len(strings.TrimRight(prefix, " \t"))
		e.eo = lineEnd
		// CR принадлежит переводу строки, а не комментарию: срежем - получим
		// смешанные концы строк в CRLF-файле.
		if e.eo > e.so && src[e.eo-1] == '\r' {
			e.eo--
		}
	}
	return e, nil
}

// placeholder - текст из шаблона команды, а не правка. Без проверки слабая
// модель копирует команду целиком и превращает комментарий в "...".
func placeholder(s string) bool {
	t := strings.TrimSpace(s)
	return t == "" || strings.Trim(t, ".") == "" || t == "<new text>"
}

func crlf(src string) bool { return strings.Contains(src, "\r\n") }

func nl(crlf bool) string {
	if crlf {
		return "\r\n"
	}
	return "\n"
}

// renderComment собирает комментарий обратно. Маркер не меняется: из <!-- -->
// нельзя сделать // - разметка выведет его на экран, а из /** нельзя /*.
func renderComment(indent, head, body string, maxLine int) []string {
	t := strings.TrimLeft(head, " \t")
	switch {
	case strings.HasPrefix(t, "<!--"):
		return renderFenced(indent, "<!--", "-->", "  ", body, maxLine)
	case strings.HasPrefix(t, "/**"):
		return renderFenced(indent, "/**", "*/", " * ", body, maxLine)
	case strings.HasPrefix(t, "/*"):
		return renderFenced(indent, "/*", "*/", " * ", body, maxLine)
	}
	marker := "//"
	switch {
	case strings.HasPrefix(t, "///"):
		marker = "///"
	case strings.HasPrefix(t, "#"):
		marker = "#"
	}
	out := make([]string, 0, 4)
	for _, ch := range wrap(body, lineWidth(maxLine, len(indent)+len(marker)+1)) {
		out = append(out, indent+marker+" "+ch)
	}
	return out
}

// renderFenced - комментарий с открывающим и закрывающим маркером. В одну
// строку он собирается, только если вместе с закрывающим влезает в maxLine.
func renderFenced(indent, open, close, inner, body string, maxLine int) []string {
	chunks := wrap(body, lineWidth(maxLine, len(indent)+len(inner)))
	if len(chunks) == 1 && len(indent)+len(open)+len(close)+2+len([]rune(chunks[0])) <= maxLine {
		return []string{indent + open + " " + chunks[0] + " " + close}
	}
	out := []string{indent + open}
	for _, ch := range chunks {
		out = append(out, indent+inner+ch)
	}
	tail := close
	if strings.HasSuffix(inner, "* ") {
		tail = " " + close
	}
	return append(out, indent+tail)
}

func lineWidth(maxLine, lead int) int {
	if w := maxLine - lead; w > 20 {
		return w
	}
	return 20
}

func wrap(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	line := words[0]
	for _, w := range words[1:] {
		if len([]rune(line))+1+len([]rune(w)) > width {
			out = append(out, line)
			line = w
			continue
		}
		line += " " + w
	}
	return append(out, line)
}

// codeSets - правила и пороги длины те же, что у основного прогона: иначе в
// снимок попадёт не то правило, которое видел автор в отчёте.
func codeSets(cfg config.Config) (code.CodeSets, error) {
	cs := code.CodeSets{MaxLines: 2, MaxLineLen: 100}
	if cfg.Comments.MaxLines != nil {
		cs.MaxLines = *cfg.Comments.MaxLines
	}
	if cfg.Comments.MaxLine != nil {
		cs.MaxLineLen = *cfg.Comments.MaxLine
	}
	var err error
	for _, set := range []struct {
		dst  **humanize.RuleSet
		name string
	}{{&cs.RU, "ru"}, {&cs.EN, "en"}, {&cs.Code, "code"}} {
		if *set.dst, err = humanize.LoadBuiltin(set.name); err != nil {
			return cs, err
		}
	}
	return cs, nil
}

// keepBlocks вносит блоки в снимок: автор посмотрел и решил оставить как есть.
// Пишутся те правила, что срабатывают сейчас, - поправят текст, находка вернётся.
func keepBlocks(root, basePath string, cmts []code.Comment, write bool) int {
	cfg, err := config.Find(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	path := basePath
	if path == "" && cfg.Baseline != "" {
		path = cfg.Baseline
		if cfg.Path != "" && !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(cfg.Path), path)
		}
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "--keep: give --baseline <file> or baseline in .hum1izer.yaml")
		return 2
	}
	if cfg.Baseline == "" {
		fmt.Fprintf(os.Stderr, "baseline %s: a run without --baseline won't read it, add baseline to .hum1izer.yaml\n", path)
	}
	set, err := baseline.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cs, err := codeSets(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	added := 0
	for _, c := range cmts {
		hash := baseline.Hash(c.Text)
		rules := code.CheckComment(cs, c, "code")
		if len(rules) == 0 {
			fmt.Printf("= %s %s: no findings, nothing to add to the baseline\n", hash, c.File)
			continue
		}
		n := 0
		for _, f := range rules {
			if set.Add(baseline.Entry{Hash: hash, Rule: f.Rule}) {
				n++
			}
		}
		added += n
		fmt.Printf("keep %s %s (+%d)\n", hash, c.File, n)
	}
	if !write {
		fmt.Printf("\npreview: --write will write %d lines to %s\n", added, path)
		return 0
	}
	if err := set.Save(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("baseline %s: +%d\n", path, added)
	return 0
}

// fixAuto удаляет то, что чинится без модели. Список - deleteOnly, значение
// true там и означает "можно без модели".
func fixAuto(root string, write bool) int {
	cfg, err := config.Find(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cs, err := codeSets(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	paths, err := walkPaths(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var edits []edit
	for _, p := range paths {
		cmts, err := code.ExtractComments(p)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, c := range cmts {
			hit := false
			for _, f := range code.CheckComment(cs, c, "code") {
				if deleteOnly[f.Rule] || deleteOnly[f.Category] {
					hit = true
				}
			}
			if !hit {
				continue
			}
			e, err := plan(c, string(raw), true, "", 0)
			if err != nil {
				continue
			}
			edits = append(edits, e)
		}
	}
	return apply(edits, write)
}

// apply правит файлы с конца, чтобы смещения выше правки не съезжали.
func apply(edits []edit, write bool) int {
	byFile := map[string][]edit{}
	for _, e := range edits {
		byFile[e.file] = append(byFile[e.file], e)
	}
	if len(edits) == 0 {
		fmt.Println("nothing to edit")
		return 0
	}
	for file, es := range byFile {
		sortEditsDesc(es)
		for i := len(es) - 1; i >= 0; i-- {
			e := es[i]
			for j, l := range e.old {
				fmt.Printf("- %s:%d %s\n", file, e.line+j, l)
			}
			for j, l := range strings.Split(e.new, "\n") {
				if e.new == "" {
					break
				}
				fmt.Printf("+ %s:%d %s\n", file, e.line+j, strings.TrimSuffix(l, "\r"))
			}
		}
		if !write {
			continue
		}
		if err := writeEdits(file, es); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if !write {
		fmt.Println("\npreview: --write will apply")
	}
	return 0
}

// writeEdits правит один файл и ничего не печатает: в TUI печать в stdout
// ложится поверх экрана и ломает верстку.
func writeEdits(file string, es []edit) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	src := string(raw)
	sortEditsDesc(es)
	for _, e := range es {
		if e.so < 0 || e.eo > len(src) || e.so > e.eo {
			return fmt.Errorf("%s: range %d-%d outside file", file, e.so, e.eo)
		}
		src = src[:e.so] + e.new + src[e.eo:]
	}
	return os.WriteFile(file, []byte(src), 0o644)
}

// keepOne кладёт один блок в снимок, тоже молча. Правила берутся из самой
// находки: проверять блок заново незачем, он уже проверен.
func keepOne(basePath, hash string, rules []string) error {
	set, err := baseline.Load(basePath)
	if err != nil {
		return err
	}
	for _, r := range rules {
		set.Add(baseline.Entry{Hash: hash, Rule: r})
	}
	return set.Save()
}

// sortEditsDesc - правки по убыванию смещения: так ранние не сдвигают поздние.
func sortEditsDesc(es []edit) {
	sort.Slice(es, func(i, j int) bool { return es[i].so > es[j].so })
}
