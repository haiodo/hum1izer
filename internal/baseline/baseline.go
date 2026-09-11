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

const header = "# hum1izer baseline v1\n" +
	"# Снимок находок. Прогон с --baseline ругается только на то, чего здесь нет.\n" +
	"# Пересоздать: hum1izer --code --baseline <файл> --write-baseline <путь>\n" +
	"# Формат: <хэш комментария>\\t<правило>\\t<файл>\n"

// Entry - одна известная находка.
type Entry struct {
	Hash, Rule, File string
}

func (e Entry) Key() string { return e.Hash + "\t" + e.Rule + "\t" + e.File }

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
	for line := 1; sc.Scan(); line++ {
		t := sc.Text()
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if len(strings.Split(t, "\t")) != 3 {
			return nil, fmt.Errorf("%s:%d: ожидалось три поля через табуляцию", path, line)
		}
		s.keys[t] = true
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

// Write пишет снимок отсортированным: файл кладётся в git, диффы должны читаться.
func Write(path string, entries []Entry) error {
	uniq := map[string]Entry{}
	for _, e := range entries {
		uniq[e.Key()] = e
	}
	sorted := make([]Entry, 0, len(uniq))
	for _, e := range uniq {
		sorted = append(sorted, e)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].File != sorted[j].File {
			return sorted[i].File < sorted[j].File
		}
		if sorted[i].Rule != sorted[j].Rule {
			return sorted[i].Rule < sorted[j].Rule
		}
		return sorted[i].Hash < sorted[j].Hash
	})

	var b strings.Builder
	b.WriteString(header)
	for _, e := range sorted {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", e.Hash, e.Rule, e.File)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
