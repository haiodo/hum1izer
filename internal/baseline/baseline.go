// Package baseline - снимок находок проекта. Прогон сверяется со снимком и
// ругается только на новое: так инструмент встраивается в CI на репозитории,
// где находок тысячи, и не даёт им прибавляться.
package baseline

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const header = "# hum1izer baseline v3\n" +
	"# Снимок находок. Прогон с --baseline ругается только на то, чего здесь нет.\n" +
	"# Пересоздать: hum1izer --code --baseline <файл> --write-baseline <путь>\n" +
	"# Формат: <хэш комментария>\\t<код правила>[\\t<код правила>...]\n" +
	"# Коды правил расшифрованы ниже. Пути нет намеренно: ключ - текст комментария,\n" +
	"# поэтому перенос кода в другой файл находку не воскрешает. Обратная сторона:\n" +
	"# копия того же комментария в новом файле тоже считается известной.\n"

// Entry - одна известная находка: правило, сработавшее на этом тексте.
type Entry struct {
	Hash, Rule string
}

func (e Entry) Key() string { return e.Hash + "\t" + Code(e.Rule) }

// Code - короткий код правила. В снимке стоит он, а не имя: имена русские и
// длинные, а файл лежит в git и читается диффом.
func Code(rule string) string {
	sum := sha256.Sum256([]byte(rule))
	return hex.EncodeToString(sum[:])[:6]
}

type Set struct {
	Path string
	keys map[string]bool
	seen map[string]bool
}

// Hash - устойчивый ключ текста комментария. Пробелы схлопываются, чтобы
// переотступ файла не считался новой находкой.
func Hash(text string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(text), " ")))
	return hex.EncodeToString(sum[:])[:12]
}

// Load читает снимок. Отсутствие файла не ошибка: это первый прогон.
func Load(path string) (*Set, error) {
	s := &Set{Path: path, keys: map[string]bool{}, seen: map[string]bool{}}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	v1, v3 := false, false
	for line := 1; sc.Scan(); line++ {
		t := sc.Text()
		switch {
		case strings.HasPrefix(t, "# hum1izer baseline v1"):
			v1 = true
		case strings.HasPrefix(t, "# hum1izer baseline v3"):
			v3 = true
		}
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		f := strings.Split(t, "\t")
		if len(f) < 2 {
			return nil, fmt.Errorf("%s:%d: ожидался хэш и хотя бы одно правило через табуляцию", path, line)
		}
		// В v1 третьим полем шёл путь к файлу; в ключ он больше не входит
		if v1 {
			f = f[:2]
		}
		for _, rule := range f[1:] {
			if !v3 {
				rule = Code(rule)
			}
			s.keys[f[0]+"\t"+rule] = true
		}
	}
	return s, sc.Err()
}

// Known отмечает находку как виденную и говорит, была ли она в снимке.
func (s *Set) Known(e Entry) bool {
	k := e.Key()
	s.seen[k] = true
	return s.keys[k]
}

func (s *Set) Size() int { return len(s.keys) }

// Fixed - находки из снимка, которых больше нет. Это прогресс, его видно.
func (s *Set) Fixed() int {
	n := 0
	for k := range s.keys {
		if !s.seen[k] {
			n++
		}
	}
	return n
}

// Write пишет снимок отсортированным, по строке на комментарий: файл кладётся в
// git, диффы должны читаться.
func Write(path string, entries []Entry) error {
	rules := map[string]map[string]bool{}
	for _, e := range entries {
		if rules[e.Hash] == nil {
			rules[e.Hash] = map[string]bool{}
		}
		rules[e.Hash][e.Rule] = true
	}
	hashes := make([]string, 0, len(rules))
	for h := range rules {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)

	legend := map[string]string{}
	for _, rs := range rules {
		for r := range rs {
			legend[Code(r)] = r
		}
	}
	codes := make([]string, 0, len(legend))
	for c := range legend {
		codes = append(codes, c)
	}
	sort.Strings(codes)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("#\n")
	for _, c := range codes {
		fmt.Fprintf(&b, "# %s %s\n", c, legend[c])
	}
	b.WriteString("#\n")
	for _, h := range hashes {
		line := make([]string, 0, len(rules[h])+1)
		line = append(line, h)
		for r := range rules[h] {
			line = append(line, Code(r))
		}
		sort.Strings(line[1:])
		fmt.Fprintln(&b, strings.Join(line, "\t"))
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
