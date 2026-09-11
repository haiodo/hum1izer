// Command lift считает подъём каждого правила на парном корпусе: во сколько раз
// маркер чаще встречается у машины, чем у человека. Результат идёт в поле lift
// в rules.yaml и задаёт вес находки.
//
// Вход - JSONL с полями lang, label ("ai" или "human") и text. Проверено на
// iitolstykh/LLMTrace_detection (Apache-2.0, ru и en, модели 2025 года).
//
//	go run ./eval/lift --file valid.jsonl --lang ru
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/haiodo/hum1izer/internal/humanize"
)

type row struct {
	Lang  string `json:"lang"`
	Label string `json:"label"`
	Text  string `json:"text"`
	Model string `json:"model"`
}

type side struct {
	docs, words int
	hits        map[string]int
}

func newSide() *side { return &side{hits: map[string]int{}} }

func main() {
	file := flag.String("file", "", "JSONL с полями lang, label, text")
	lang := flag.String("lang", "ru", "значение поля lang в корпусе")
	set := flag.String("set", "", "встроенный набор правил: ru или en, по умолчанию как lang")
	rulesPath := flag.String("rules", "", "свой файл правил вместо встроенного")
	minHits := flag.Int("min-hits", 20, "не показывать правила реже скольких попаданий суммарно")
	tsv := flag.Bool("tsv", false, "машинный вывод: имя, lift, попаданий")
	limit := flag.Int("limit", 0, "сколько строк прочитать, 0 - все")
	flag.Parse()
	if *file == "" {
		flag.Usage()
		os.Exit(2)
	}

	if *set == "" {
		*set = *lang
	}
	rs, err := loadRules(*rulesPath, *set)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	human, ai := newSide(), newSide()
	models := map[string]int{}
	if err := scan(*file, *lang, *limit, func(r row) {
		s := human
		if r.Label == "ai" {
			s = ai
			models[r.Model]++
		}
		s.docs++
		s.words += humanize.CountWords(r.Text)
		count(rs, r.Text, s.hits)
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	report(*lang, human, ai, models, *minHits, *tsv)
}

func loadRules(path, lang string) (*humanize.RuleSet, error) {
	if path != "" {
		return humanize.LoadRules(path)
	}
	return humanize.LoadBuiltin(lang)
}

func scan(path, lang string, limit int, fn func(row)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	n := 0
	for sc.Scan() {
		var r row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		// label "mixed" - текст, дописанный моделью поверх человеческого.
		// В человеческую сторону его класть нельзя: он занижает подъём.
		if r.Lang != lang || r.Text == "" || (r.Label != "ai" && r.Label != "human") {
			continue
		}
		fn(r)
		n++
		if limit > 0 && n >= limit {
			break
		}
	}
	return sc.Err()
}

// count складывает попадания по имени правила и по имени категории: категория
// нужна отдельно, потому что вес в rules.yaml ставится и на неё тоже.
func count(rs *humanize.RuleSet, text string, into map[string]int) {
	for _, h := range humanize.ScanHardBans(rs, text) {
		into[h.Marker] += h.Count
	}
	for _, h := range humanize.ScanMarkers(rs, text) {
		into[h.Marker] += h.Count
		into["["+h.Category+"]"] += h.Count
	}
}

func report(lang string, human, ai *side, models map[string]int, minHits int, tsv bool) {
	if tsv {
		reportTSV(human, ai, minHits)
		return
	}
	fmt.Printf("язык: %s\nчеловек: %d док., %d слов\nмашина:  %d док., %d слов\n",
		lang, human.docs, human.words, ai.docs, ai.words)
	fmt.Println("модели:", topKeys(models, 6))
	if human.words == 0 || ai.words == 0 {
		fmt.Println("нет одной из сторон, считать нечего")
		return
	}
	fmt.Printf("\n%-46s %8s %8s %7s\n", "правило", "чел/100к", "маш/100к", "lift")

	type res struct {
		name       string
		h, a, lift float64
		total      int
	}
	var out []res
	names := map[string]bool{}
	for n := range human.hits {
		names[n] = true
	}
	for n := range ai.hits {
		names[n] = true
	}
	for n := range names {
		total := human.hits[n] + ai.hits[n]
		if total < minHits {
			continue
		}
		h := per100k(human.hits[n], human.words)
		a := per100k(ai.hits[n], ai.words)
		l := 0.0
		if h > 0 {
			l = a / h
		}
		out = append(out, res{n, h, a, l, total})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].lift > out[j].lift })
	for _, r := range out {
		lift := fmt.Sprintf("%7.2f", r.lift)
		if r.h == 0 {
			lift = "      -" // у человека ноль, отношение не определено
		}
		fmt.Printf("%-46s %8.1f %8.1f %s\n", trim(r.name, 46), r.h, r.a, lift)
	}
}

// reportTSV: имя, lift, сколько всего попаданий. Отсюда значения переносятся
// в rules.yaml. "inf" означает ноль у человека - маркер разделяет идеально.
func reportTSV(human, ai *side, minHits int) {
	names := map[string]bool{}
	for n := range human.hits {
		names[n] = true
	}
	for n := range ai.hits {
		names[n] = true
	}
	var sorted []string
	for n := range names {
		if human.hits[n]+ai.hits[n] >= minHits {
			sorted = append(sorted, n)
		}
	}
	sort.Strings(sorted)
	for _, n := range sorted {
		h := per100k(human.hits[n], human.words)
		a := per100k(ai.hits[n], ai.words)
		lift := "inf"
		if h > 0 {
			lift = fmt.Sprintf("%.2f", a/h)
		}
		fmt.Printf("%s\t%s\t%d\n", n, lift, human.hits[n]+ai.hits[n])
	}
}

func per100k(n, words int) float64 {
	if words == 0 {
		return 0
	}
	return float64(n) / float64(words) * 100000
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func topKeys(m map[string]int, n int) []string {
	type kv struct {
		k string
		n int
	}
	var l []kv
	for k, v := range m {
		l = append(l, kv{k, v})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].n > l[j].n })
	var out []string
	for i, e := range l {
		if i >= n {
			break
		}
		out = append(out, fmt.Sprintf("%s (%d)", e.k, e.n))
	}
	return out
}
