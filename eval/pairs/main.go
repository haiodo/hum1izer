// Command pairs собирает парный корпус комментариев из одного коммита: в нём
// агент переписал комментарии, оригиналы лежат в родителе. До правки - "human",
// после - "ai". Один репозиторий, одни файлы, разница в том, кто написал текст.
//
//	go run ./eval/pairs --repo ../platform --commit f79c901006 --out corpus/f79c901006-comments.jsonl
//
// Формат строк читает eval/lift: {lang, label, text}. lang ставится по самому
// тексту, как в основном коде: доля кириллицы от 0.3 - ru, иначе en.
//
// Блоки сопоставляются по коду рядом (Comment.Next): коммит правит комментарии,
// код рядом остаётся тем же, поэтому совпадение Next - надёжнее позиций в диффе,
// которые съезжают от сокращения строк. Пара берётся только когда текст поменялся:
// нетронутые блоки не несут контраста, а добавленные или удалённые без пары
// смещают частоты в одну сторону.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/config"
	"github.com/haiodo/hum1izer/internal/humanize"
)

type row struct {
	Lang  string `json:"lang"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

func main() {
	repo := flag.String("repo", "../platform", "репозиторий с коммитом")
	commit := flag.String("commit", "", "коммит с перепиской комментариев")
	out := flag.String("out", "", "файл результата, JSONL")
	flag.Parse()
	if *commit == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}

	paths, err := changedFiles(*repo, *commit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	var rows []row
	var stats counts
	for _, p := range paths {
		if config.LangOf(p) == "" {
			continue
		}
		oldSrc, err := gitShow(*repo, *commit+"^", p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: родительская версия: %v\n", p, err)
			continue
		}
		newSrc, err := gitShow(*repo, *commit, p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: новая версия: %v\n", p, err)
			continue
		}
		oldCmts, err := extract(p, oldSrc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
			continue
		}
		newCmts, err := extract(p, newSrc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
			continue
		}
		stats.files++
		paired, same, lost := match(oldCmts, newCmts)
		stats.same += same
		stats.lost += lost
		for _, pair := range paired {
			oldC, newC := pair[0], pair[1]
			if strings.TrimSpace(oldC.Text) == "" || strings.TrimSpace(newC.Text) == "" {
				continue
			}
			rows = append(rows,
				row{langOfText(oldC.Text), "human", oldC.Text},
				row{langOfText(newC.Text), "ai", newC.Text})
			stats.pairs++
		}
	}

	// Пары идут соседними строками: human, за ним ai. Порядок файлов - как
	// вернул git, он уже детерминирован. Так корпус читается глазами, а lift
	// порядок строк не различает.
	if err := writeRows(*out, rows); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "файлов %d, пар %d, нетронуто %d, без пары %d -> %s\n",
		stats.files, stats.pairs, stats.same, stats.lost, *out)
}

type counts struct {
	files, pairs, same, lost int
}

func writeRows(path string, rows []row) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// changedFiles - изменённые файлы коммита, только правки: в добавленных и
// удалённых пары не бывает.
func changedFiles(repo, commit string) ([]string, error) {
	out, err := git(repo, "show", "--name-status", "--pretty=format:", "--no-renames", commit)
	if err != nil {
		return nil, err
	}
	var res []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimSpace(line), "\t")
		if len(f) == 2 && f[0] == "M" && f[1] != "" {
			res = append(res, f[1])
		}
	}
	sort.Strings(res)
	return res, nil
}

func gitShow(repo, rev, path string) ([]byte, error) {
	return git(repo, "show", rev+":"+path)
}

func git(repo string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// extract достаёт комментарии из байтов: ExtractComments читает с диска и
// переключается по расширению, поэтому временный файл носит то же расширение.
func extract(path string, src []byte) ([]code.Comment, error) {
	tmp, err := os.CreateTemp("", "pairs-*"+filepath.Ext(path))
	if err != nil {
		return nil, err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(src); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	return code.ExtractComments(name)
}

// match сводит старые и новые блоки одного файла по коду рядом. Возвращаются
// пары с изменившимся текстом; same - совпавшие без изменений, lost - старые
// без продолжения (удалены целиком) и новые без предшественника (добавлены).
func match(oldCmts, newCmts []code.Comment) (paired [][2]code.Comment, same, lost int) {
	queue := map[string][]code.Comment{}
	for _, c := range oldCmts {
		queue[c.Next] = append(queue[c.Next], c)
	}
	for _, n := range newCmts {
		q := queue[n.Next]
		if len(q) == 0 {
			lost++
			continue
		}
		o := q[0]
		queue[n.Next] = q[1:]
		if o.Text == n.Text {
			same++
			continue
		}
		paired = append(paired, [2]code.Comment{o, n})
	}
	for _, q := range queue {
		lost += len(q)
	}
	return paired, same, lost
}

// langOfText - тот же порог, что в code.CheckComment при выборе набора правил.
func langOfText(text string) string {
	if humanize.CyrillicShare(text) >= 0.3 {
		return "ru"
	}
	return "en"
}
