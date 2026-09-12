package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/haiodo/hum1izer/internal/baseline"
	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/humanize"
)

const fixUsage = `hum1izer fix - применить правку к блоку комментария по хэшу из отчёта.

  hum1izer fix --block <хэш> --delete путь        удалить блок целиком
  hum1izer fix --block <хэш> --text "..." путь    заменить текст блока
  hum1izer fix --block <хэш> --text - путь        то же, текст со stdin
  hum1izer fix --auto путь                        удалить механическое
  hum1izer fix --batch - путь                     пачкой: JSONL со stdin

  --write     применить; без него печатается только предпросмотр
  --max-line  переносить длинные строки при замене (100)

Пачка - по строке JSON на правку, дерево обходится один раз:

  {"block":"55dc2ad977ba","text":"новый текст комментария"}
  {"block":"a832aeaac8cf","delete":true}

Блок ищется по хэшу, а не по номеру строки: после первой же правки строки
съезжают, хэш нет. Текст задаётся без // и /* */ - маркеры, отступ и перенос
инструмент восстановит сам.
`

func runFix(args []string) int {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, fixUsage) }
	block := fs.String("block", "", "хэш блока из отчёта")
	del := fs.Bool("delete", false, "удалить блок целиком")
	text := fs.String("text", "", "новый текст комментария, - читает stdin")
	auto := fs.Bool("auto", false, "удалить механическое: закомментированный код и комментарии-пустышки")
	batch := fs.String("batch", "", "файл JSONL с правками, - читает stdin")
	write := fs.Bool("write", false, "применить правку")
	maxLine := fs.Int("max-line", 100, "переносить длинные строки при замене")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}

	switch {
	case *batch != "":
		return fixBatch(root, *batch, *write, *maxLine)
	case *auto:
		return fixAuto(root, *write)
	case *block == "":
		fs.Usage()
		return 2
	case *del && *text != "":
		fmt.Fprintln(os.Stderr, "--delete и --text вместе не имеют смысла")
		return 2
	case !*del && *text == "":
		fmt.Fprintln(os.Stderr, "нужен --delete или --text")
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

	c, err := locate(root, *block)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	e, err := plan(c, *del, body, *maxLine)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return apply([]edit{e}, *write)
}

type batchEdit struct {
	Block  string `json:"block"`
	Text   string `json:"text"`
	Delete bool   `json:"delete"`
}

// fixBatch применяет пачку правок за один обход дерева: на большом репозитории
// обход стоит секунды, и делать его на каждую правку незачем.
func fixBatch(root, src string, write bool, maxLine int) int {
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
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan(); line++ {
		t := strings.TrimSpace(sc.Text())
		if t == "" {
			continue
		}
		var b batchEdit
		if err := json.Unmarshal([]byte(t), &b); err != nil {
			fmt.Fprintf(os.Stderr, "строка %d: %v\n", line, err)
			return 2
		}
		if b.Block == "" || (b.Text == "" && !b.Delete) {
			fmt.Fprintf(os.Stderr, "строка %d: нужен block и text или delete\n", line)
			return 2
		}
		want[b.Block] = b
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	paths, err := code.WalkCode(root, code.Filter{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var edits []edit
	found := map[string]bool{}
	for _, p := range paths {
		cmts, err := code.ExtractComments(p)
		if err != nil {
			continue
		}
		for _, c := range cmts {
			b, ok := want[baseline.Hash(c.Text)]
			if !ok {
				continue
			}
			e, err := plan(c, b.Delete, b.Text, maxLine)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", b.Block, err)
				continue
			}
			found[b.Block] = true
			edits = append(edits, e)
		}
	}
	exit := 0
	for h := range want {
		if !found[h] {
			fmt.Fprintf(os.Stderr, "блок %s не найден\n", h)
			exit = 2
		}
	}
	if rc := apply(edits, write); rc != 0 {
		return rc
	}
	return exit
}

// edit - замена строк [from, to) файла новыми. Пустой lines означает удаление.
type edit struct {
	file     string
	from, to int
	old, new []string
}

func locate(root, hash string) (code.Comment, error) {
	paths, err := code.WalkCode(root, code.Filter{})
	if err != nil {
		return code.Comment{}, err
	}
	for _, p := range paths {
		cmts, err := code.ExtractComments(p)
		if err != nil {
			continue
		}
		for _, c := range cmts {
			if baseline.Hash(c.Text) == hash {
				return c, nil
			}
		}
	}
	return code.Comment{}, fmt.Errorf("блок %s не найден в %s: текст изменился или путь не тот", hash, root)
}

func plan(c code.Comment, del bool, body string, maxLine int) (edit, error) {
	src, err := os.ReadFile(c.File)
	if err != nil {
		return edit{}, err
	}
	lines := strings.Split(string(src), "\n")
	if c.Start < 1 || c.End > len(lines) {
		return edit{}, fmt.Errorf("%s: блок вне файла, строки %d-%d", c.File, c.Start, c.End)
	}
	from, to := c.Start-1, c.End
	old := lines[from:to]

	head := strings.Split(c.Raw, "\n")[0]
	// Комментарий в хвосте строки кода: трогаем только его часть, код остаётся.
	if cut := strings.Index(lines[from], strings.TrimSpace(head)); cut > 0 && strings.TrimSpace(lines[from][:cut]) != "" {
		if !del {
			return edit{}, fmt.Errorf("замена хвостового комментария не поддерживается, правь его вручную")
		}
		kept := strings.TrimRight(lines[from][:cut], " \t")
		return edit{file: c.File, from: from, to: to, old: old, new: []string{kept}}, nil
	}
	if del {
		return edit{file: c.File, from: from, to: to, old: old}, nil
	}
	return edit{file: c.File, from: from, to: to, old: old, new: renderComment(lines[from], body, maxLine)}, nil
}

// renderComment собирает комментарий обратно: отступ и маркер берутся из исходного
// блока, текст переносится по maxLine.
func renderComment(head, body string, maxLine int) []string {
	indent := head[:len(head)-len(strings.TrimLeft(head, " \t"))]
	marker := "//"
	switch t := strings.TrimLeft(head, " \t"); {
	case strings.HasPrefix(t, "///"):
		marker = "///"
	case strings.HasPrefix(t, "#"):
		marker = "#"
	case strings.HasPrefix(t, "/*"):
		marker = "*"
	}

	width := maxLine - len(indent) - len(marker) - 1
	if width < 20 {
		width = 20
	}
	chunks := wrap(body, width)

	if marker != "*" {
		out := make([]string, 0, len(chunks))
		for _, ch := range chunks {
			out = append(out, indent+marker+" "+ch)
		}
		return out
	}
	if len(chunks) == 1 {
		return []string{indent + "/* " + chunks[0] + " */"}
	}
	out := []string{indent + "/**"}
	for _, ch := range chunks {
		out = append(out, indent+" * "+ch)
	}
	return append(out, indent+" */")
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

// fixAuto удаляет то, что чинится без модели: закомментированный код и
// комментарии-пустышки вроде "// constructor".
var autoRules = map[string]bool{
	"Закомментированный код": true,
	"Комментарий-пустышка":   true,
}

func fixAuto(root string, write bool) int {
	cs := code.CodeSets{}
	var err error
	for _, set := range []struct {
		dst  **humanize.RuleSet
		name string
	}{{&cs.RU, "ru"}, {&cs.EN, "en"}, {&cs.Code, "code"}} {
		if *set.dst, err = humanize.LoadBuiltin(set.name); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	paths, err := code.WalkCode(root, code.Filter{})
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
		for _, c := range cmts {
			hit := false
			for _, f := range code.CheckComment(cs, c, "code") {
				if autoRules[f.Rule] || autoRules[f.Category] {
					hit = true
				}
			}
			if !hit {
				continue
			}
			e, err := plan(c, true, "", 0)
			if err != nil {
				continue
			}
			edits = append(edits, e)
		}
	}
	return apply(edits, write)
}

// apply правит файлы снизу вверх, чтобы номера строк выше правки не съезжали.
func apply(edits []edit, write bool) int {
	byFile := map[string][]edit{}
	for _, e := range edits {
		byFile[e.file] = append(byFile[e.file], e)
	}
	if len(edits) == 0 {
		fmt.Println("нечего править")
		return 0
	}
	for file, es := range byFile {
		for i := len(es) - 1; i >= 0; i-- {
			e := es[i]
			for i, l := range e.old {
				fmt.Printf("- %s:%d %s\n", file, e.from+1+i, l)
			}
			for i, l := range e.new {
				fmt.Printf("+ %s:%d %s\n", file, e.from+1+i, l)
			}
		}
		if !write {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		lines := strings.Split(string(src), "\n")
		sortEditsDesc(es)
		for _, e := range es {
			lines = append(lines[:e.from], append(append([]string{}, e.new...), lines[e.to:]...)...)
		}
		if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if !write {
		fmt.Println("\nпредпросмотр: --write применит")
	}
	return 0
}

func sortEditsDesc(es []edit) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && es[j].from > es[j-1].from; j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}
