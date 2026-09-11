package code

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/haiodo/hum1izer/internal/config"
)

// Comment - блок комментария или текст коммита: то, что мы проверяем как прозу.
type Comment struct {
	File  string `json:"file"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Lines int    `json:"lines"`
	Text  string `json:"text"`
	Raw   string `json:"raw"`  // исходные строки как есть, вместе с // и /* */
	Lang  string `json:"lang"` // go, ts, svelte, swift, git
	Next  string `json:"-"`    // первая строка кода после блока, для проверки пересказа
	Doc   bool   `json:"-"`    // /** */, /// или /// - документирующий комментарий
}

// ID - устойчивый ключ блока для агента: по нему он сверяет, ушла ли находка
// после правки. Строки съезжают, поэтому ключ дополняется текстом в Raw.
func (c Comment) ID() string {
	if c.Start == c.End || c.End == 0 {
		return fmt.Sprintf("%s:%d", c.File, c.Start)
	}
	return fmt.Sprintf("%s:%d-%d", c.File, c.Start, c.End)
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
	"bin": true, "gen": true, "coverage": true, "testdata": true, ".svelte-kit": true,
	".next": true, "target": true, "Pods": true, ".reports": true,
}

var generatedRe = regexp.MustCompile(`(?m)^(//|#|/\*) *Code generated .* DO NOT EDIT|@generated|eslint-disable`)

// Filter - что из дерева брать. Пустой Filter означает все поддерживаемые языки.
type Filter struct {
	SkipTests bool
	Langs     map[string]bool // nil - все языки
	Exclude   []*regexp.Regexp
}

func (f Filter) allow(root, path string) bool {
	lang := config.LangOf(path)
	if lang == "" {
		return false
	}
	if f.Langs != nil && !f.Langs[lang] {
		return false
	}
	name := filepath.Base(path)
	if strings.HasSuffix(name, ".d.ts") || strings.HasSuffix(name, ".pb.go") {
		return false
	}
	if f.SkipTests && (strings.HasSuffix(name, "_test.go") ||
		strings.Contains(name, ".test.") || strings.Contains(name, ".spec.")) {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)
	for _, re := range f.Exclude {
		if re.MatchString(rel) || re.MatchString(name) {
			return false
		}
	}
	return true
}

// WalkCode собирает файлы с поддерживаемыми расширениями. Файл передаётся как есть.
func WalkCode(root string, f Filter) ([]string, error) {
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return []string{root}, nil
	}
	var out []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // нечитаемый каталог пропускаем, обход не роняем
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return fs.SkipDir
			}
			return nil
		}
		if f.allow(root, path) {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// ExtractComments достаёт блоки комментариев из файла. Для Go берём go/parser,
// для остальных - посимвольный сканер: он знает про строки и шаблоны, поэтому
// "//" внутри литерала за комментарий не считает.
func ExtractComments(path string) ([]Comment, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if generatedRe.Match(src[:min(len(src), 2048)]) {
		return nil, nil
	}
	lines := strings.Split(string(src), "\n")

	var spans []span
	switch filepath.Ext(path) {
	case ".go":
		if spans, err = goSpans(path, src); err != nil {
			spans = scanC(string(src), false) // файл не парсится - падаем на сканер
		}
	case ".svelte":
		spans = scanSvelte(string(src))
	case ".swift":
		spans = scanC(string(src), false)
	default:
		spans = scanC(string(src), true)
	}

	lang := strings.TrimPrefix(filepath.Ext(path), ".")
	out := make([]Comment, 0, len(spans))
	for _, s := range merge(spans) {
		raw := lines[s.start-1 : minInt(s.end, len(lines))]
		out = append(out, Comment{
			File:  path,
			Start: s.start,
			End:   s.end,
			Lines: len(raw),
			Text:  stripMarkers(raw),
			Raw:   strings.Join(raw, "\n"),
			Lang:  lang,
			Next:  nextCode(lines, s.end),
			Doc:   isDoc(raw),
		})
	}
	return out, nil
}

type span struct{ start, end int }

func goSpans(path string, src []byte) ([]span, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var out []span
	for _, cg := range f.Comments {
		out = append(out, span{fset.Position(cg.Pos()).Line, fset.Position(cg.End()).Line})
	}
	return out, nil
}

// scanC - посимвольный проход по C-подобному исходнику. regexLit включается
// только для JS/TS: в Go и Swift литералов регулярок нет, и эвристика их деления
// на "/" только вредила бы.
func scanC(src string, regexLit bool) []span {
	var out []span
	line, i, n := 1, 0, len(src)
	prev := byte(0)
	at := func(k int) byte {
		if k < n {
			return src[k]
		}
		return 0
	}
	for i < n {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == '/' && at(i+1) == '/':
			start := line
			for i < n && src[i] != '\n' {
				i++
			}
			out = append(out, span{start, line})
		case c == '/' && at(i+1) == '*':
			start := line
			i += 2
			for i < n && (src[i] != '*' || at(i+1) != '/') {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
			out = append(out, span{start, line})
		case c == '"' || c == '\'':
			q := c
			i++
			for i < n && src[i] != q {
				switch src[i] {
				case '\\':
					i++
				case '\n':
					line++
				}
				i++
			}
			i++
			prev = q
		case c == '`':
			i++
			depth := 0
			for i < n {
				switch {
				case src[i] == '\\':
					i += 2
					continue
				case src[i] == '\n':
					line++
				case src[i] == '`' && depth == 0:
					i++
					depth = -1
				case src[i] == '$' && at(i+1) == '{':
					depth++
					i += 2
					continue
				case src[i] == '}' && depth > 0:
					depth--
				}
				if depth < 0 {
					break
				}
				i++
			}
			prev = '`'
		case regexLit && c == '/' && regexAllowed(prev):
			j, ok, inClass := i+1, false, false
			for j < n && src[j] != '\n' {
				if src[j] == '\\' {
					j += 2
					continue
				}
				switch src[j] {
				case '[':
					inClass = true
				case ']':
					inClass = false
				case '/':
					if !inClass {
						ok = true
					}
				}
				if ok {
					break
				}
				j++
			}
			if ok {
				i, prev = j+1, '/'
			} else {
				i++
			}
		default:
			if c != ' ' && c != '\t' && c != '\r' {
				prev = c
			}
			i++
		}
	}
	return out
}

// regexAllowed: "/" начинает регулярку, если предыдущий значимый символ - не
// идентификатор и не закрывающая скобка. Грубо, но для поиска комментариев хватает.
func regexAllowed(prev byte) bool {
	if prev == 0 {
		return true
	}
	return (prev < 'a' || prev > 'z') && (prev < 'A' || prev > 'Z') &&
		(prev < '0' || prev > '9') && strings.IndexByte("_$)]}'\"`", prev) < 0
}

var (
	svelteScriptRe = regexp.MustCompile(`(?s)<(script|style)\b[^>]*>(.*?)</(script|style)>`)
	htmlCommentRe  = regexp.MustCompile(`(?s)<!--.*?-->`)
)

// scanSvelte: <script>/<style> сканируем как C, разметку - только на <!-- -->.
func scanSvelte(src string) []span {
	var out []span
	for _, m := range svelteScriptRe.FindAllStringIndex(src, -1) {
		base := strings.Count(src[:m[0]], "\n")
		for _, s := range scanC(src[m[0]:m[1]], true) {
			out = append(out, span{s.start + base, s.end + base})
		}
	}
	for _, m := range htmlCommentRe.FindAllStringIndex(src, -1) {
		start := strings.Count(src[:m[0]], "\n") + 1
		out = append(out, span{start, start + strings.Count(src[m[0]:m[1]], "\n")})
	}
	return out
}

// merge склеивает соседние комментарии в один блок: шапка из десяти // строк -
// это один текст, а не десять находок.
func merge(spans []span) []span {
	if len(spans) == 0 {
		return nil
	}
	sorted := append([]span(nil), spans...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].start < sorted[j-1].start; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	out := []span{sorted[0]}
	for _, s := range sorted[1:] {
		last := &out[len(out)-1]
		if s.start <= last.end+1 {
			if s.end > last.end {
				last.end = s.end
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

var markerRe = regexp.MustCompile(`^\s*(///?/?|/\*+|\*+/|\*|<!--|-->|#)\s?`)

func stripMarkers(lines []string) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = markerRe.ReplaceAllString(l, "")
		l = strings.TrimSuffix(strings.TrimRight(l, " \t"), "*/")
		l = strings.TrimSuffix(strings.TrimRight(l, " \t"), "-->")
		out = append(out, strings.TrimRight(l, " \t"))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func isDoc(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	first := strings.TrimSpace(lines[0])
	return strings.HasPrefix(first, "/**") || strings.HasPrefix(first, "///")
}

func nextCode(lines []string, end int) string {
	for i := end; i < len(lines) && i < end+3; i++ {
		s := strings.TrimSpace(lines[i])
		if s == "" || strings.HasPrefix(s, "//") || strings.HasPrefix(s, "*") {
			continue
		}
		return s
	}
	return ""
}

// --- коммиты --------------------------------------------------------------

const commitSep = "\x1e"

// GitCommits читает последние limit коммитов. Каждый становится Comment с
// File вида "commit a1b2c3d" - дальше он проверяется как обычная проза.
func GitCommits(dir string, limit int) ([]Comment, error) {
	if !isGitRepo(dir) {
		return nil, nil
	}
	cmd := exec.Command("git", "-C", dir, "log", "--no-merges",
		"-n", strconv.Itoa(limit), "--format=%h%x1f%s%x1f%b"+commitSep)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	var res []Comment
	for _, rec := range strings.Split(string(out), commitSep) {
		parts := strings.SplitN(strings.TrimSpace(rec), "\x1f", 3)
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		text := strings.TrimSpace(parts[1] + "\n\n" + strings.TrimSpace(parts[2]))
		res = append(res, Comment{
			File:  "commit " + parts[0],
			Start: 1,
			Lines: strings.Count(text, "\n") + 1,
			Text:  text,
			Raw:   text,
			Lang:  "git",
		})
	}
	return res, nil
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
