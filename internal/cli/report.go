package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/humanize"
)

// --- проза ----------------------------------------------------------------

func countHits(hits []humanize.Hit) int {
	n := 0
	for _, h := range hits {
		n += h.Count
	}
	return n
}

// splitBans: тире считается отдельно. В балл оно идёт по плотности с допуском,
// поэтому "банов: 72" рядом с "87/100 чисто" читалось как противоречие.
func splitBans(hits []humanize.Hit) (phrases, dashes int) {
	for _, h := range hits {
		if strings.Contains(h.Marker, "dash") {
			dashes += h.Count
			continue
		}
		phrases += h.Count
	}
	return
}

func printJSON(src string, rep humanize.Report) {
	out := struct {
		Source string `json:"source"`
		humanize.Report
	}{src, rep}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
}

func printReport(rs *humanize.RuleSet, src string, rep humanize.Report, top int) {
	t := rs.Thresholds
	fmt.Printf("=== hum1izer: %s ===\n", src)
	if rep.Genre != "marketing" {
		fmt.Printf("genre: %s (muted by genre: %d bans, %d soft)\n",
			rep.Genre, rep.MutedBans, rep.MutedSoft)
	}
	if rep.NotRussian {
		fmt.Println("⚠ text doesn't look like Russian: metrics are tuned for Russian, the report isn't meaningful")
	}
	fmt.Println()

	fmt.Printf("CLEANLINESS: %d/100  [%s]\n", rep.Score.Value, rep.Score.Band)
	fmt.Println("  (≥85 clean · 60-84 spot fix · <60 rewrite)")
	if len(rep.Score.Penalties) == 0 {
		fmt.Println("  no penalties")
	}
	for _, p := range rep.Score.Penalties {
		fmt.Printf("  %+d  %s\n", p.Points, p.Reason)
	}
	for _, n := range rep.Score.Notes {
		fmt.Printf("  ℹ  %s\n", n)
	}
	fmt.Println()

	fmt.Println("HARD BANS:")
	if len(rep.HardBans) == 0 {
		fmt.Println("  ✓ clean")
	}
	for _, h := range rep.HardBans {
		fmt.Printf("  ⛔ %s ×%d (%s)\n", h.Marker, h.Count, lineList(h.Lines))
		fmt.Printf("     → %s\n", h.Fix)
	}
	fmt.Println()

	fmt.Printf("MARKERS: %s\n", humanize.MarkerVerdict(rs, rep.Markers, rep.Rhythm.Words))
	sorted := slices.Clone(rep.Markers)
	slices.SortStableFunc(sorted, func(a, b humanize.Hit) int { return b.Count - a.Count })
	shown := sorted
	if top > 0 && len(sorted) > top {
		shown = sorted[:top]
	}
	for _, h := range shown {
		fmt.Printf("  • [%s] \"%s\" ×%d (%s)\n", h.Category, h.Marker, h.Count, lineList(h.Lines))
	}
	if len(sorted) > len(shown) {
		fmt.Printf("  ... and %d more\n", len(sorted)-len(shown))
	}
	// Рекомендации по категориям: одна строка на категорию, а не на каждое попадание.
	if fixes := categoryFixes(sorted); len(fixes) > 0 {
		fmt.Println("\n  What to do:")
		for _, f := range fixes {
			fmt.Printf("  → [%s] %s\n", f[0], f[1])
		}
	}
	fmt.Println()

	r := rep.Rhythm
	fmt.Println("RHYTHM AND TYPOGRAPHY:")
	target := rs.CVTarget(rep.Genre)
	if r.CVLen < target && r.Sentences >= 4 {
		fmt.Printf("  ⚠ flat rhythm (CV=%.3f, target ≥%.2f for genre %s): vary sentence length\n",
			r.CVLen, target, rep.Genre)
	} else {
		fmt.Printf("  ✓ rhythm varies (CV=%.3f)\n", r.CVLen)
	}
	fmt.Printf("  sentences: %d, average length: %.1f (min %d / max %d)\n",
		r.Sentences, r.MeanLen, r.MinLen, r.MaxLen)
	fmt.Printf("  words: %d, dashes: %d, ellipses: %d, parens: %d, questions: %d\n",
		r.Words, r.EmDash, r.Ellipsis, r.Parentheses, r.Questions)
	fmt.Printf("  average word length: %.2f, words 10+ letters: %.1f%%\n",
		r.MeanWordLen, r.LongWords)
	fmt.Println()

	fmt.Println("NOMINALIZATION:")
	if rep.Morph.Per100 > t.NominalTarget {
		fmt.Printf("  ⚠ deverbal nouns: %.1f per 100 words (target ≤%.1f)\n",
			rep.Morph.Per100, t.NominalTarget)
		fmt.Println("     → expand into a verb: \"carry out the implementation\" → \"implement\"")
	} else {
		fmt.Printf("  ✓ deverbal nouns: %.1f per 100 words\n", rep.Morph.Per100)
	}
	fmt.Println()

	st := rep.Structure
	fmt.Println("STRUCTURE:")
	fmt.Printf("  paragraphs: %d, length CV: %.3f, list items: %d\n",
		st.Paragraphs, st.ParaCV, st.ListItems)
	if st.Paragraphs >= t.ParaMinCount && st.ParaCV < t.ParaCVAI {
		fmt.Println("  ⚠ paragraphs are all the same length → let length follow the thought, not a template")
	}
	if st.ListItems >= t.ListicleMinItems && st.ListicleShare > t.ListicleShareAI {
		fmt.Printf("  ⚠ listicle (%d%% of lines are items) → expand some items into prose\n",
			int(st.ListicleShare*100))
	}
	if st.TitleCaseHeads > 0 {
		fmt.Printf("  ⚠ Title Case in headings: %d → in Russian only the first word is capitalized\n",
			st.TitleCaseHeads)
	}
	if st.TruncatedEnding {
		fmt.Println("  ⚠ text is cut off mid-word → finish the ending")
	}
	if len(st.Repeats) > 0 {
		fmt.Printf("  ⚠ repeated 3-word phrases: %d (%.1f per 1000 words) → rephrase\n",
			len(st.Repeats), st.RepeatPer1000)
		for i, r := range st.Repeats {
			if i >= 5 {
				fmt.Printf("     ... and %d more\n", len(st.Repeats)-i)
				break
			}
			fmt.Printf("     \"%s\" ×%d\n", r.Phrase, r.Count)
		}
	}
	fmt.Println()
}

func categoryFixes(hits []humanize.Hit) [][2]string {
	var out [][2]string
	seen := map[string]bool{}
	for _, h := range hits {
		if h.Fix == "" || seen[h.Category] {
			continue
		}
		seen[h.Category] = true
		out = append(out, [2]string{h.Category, h.Fix})
	}
	return out
}

func lineList(lines []int) string {
	const limit = 3
	shown := lines
	suffix := ""
	if len(lines) > limit {
		shown = lines[:limit]
		suffix = fmt.Sprintf(", +%d", len(lines)-limit)
	}
	parts := make([]string, len(shown))
	for i, n := range shown {
		parts[i] = strconv.Itoa(n)
	}
	return "line " + strings.Join(parts, ", ") + suffix
}

// --- код ------------------------------------------------------------------

func countFindings(items []code.Item) int {
	n := 0
	for _, it := range items {
		n += len(it.Findings)
	}
	return n
}

func cut(items []code.Item, limit int) []code.Item {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

// printJSONL - формат для агента: одна строка на блок, внутри исходный текст и
// находки. Сводка идёт в stderr, чтобы stdout оставался чистым JSONL.
func printJSONL(items []code.Item, limit, files, blocks int) {
	shown := cut(items, limit)
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	for _, it := range shown {
		_ = enc.Encode(it)
	}
	fmt.Fprintf(os.Stderr, "remaining=%d shown=%d findings=%d comments=%d files=%d\n",
		len(items), len(shown), countFindings(items), blocks, files)
}

// printMarkdown - формат для агента. В JSON текст экранирован, и чтобы заменить
// его точным совпадением, модели пришлось бы развернуть экранирование в уме.
func printMarkdown(items []code.Item, limit, files, blocks int) {
	shown := cut(items, limit)
	if len(shown) > 0 {
		fmt.Print(mdHowTo)
	}
	copies := map[string]int{}
	for _, it := range items {
		copies[it.Hash]++
	}
	for _, it := range shown {
		fmt.Printf("## %s\n", it.ID)
		fmt.Printf("block %s | `%s` | %s | weight %d", it.Hash, it.Lang, it.Kind, it.Score)
		if copies[it.Hash] > 1 {
			fmt.Printf(" | text copies: %d", copies[it.Hash])
		}
		fmt.Print("\n\n")
		for _, g := range groupFindings(it.Findings) {
			mark := ""
			if g.hard {
				mark = "hard, "
			}
			fmt.Printf("- **%s** (%sline %s)", g.rule, mark, joinInts(g.lines))
			if g.fix != "" {
				fmt.Printf(" - %s", g.fix)
			}
			fmt.Println()
		}
		fence := fenceFor(it.Raw)
		fmt.Printf("\n%s\n%s\n%s\n\n", fence, it.Raw, fence)
		if it.Kind == "comment" {
			fmt.Printf("%s\n\n", fixCommand(it))
		}
	}
	fmt.Fprintf(os.Stderr, "remaining=%d shown=%d findings=%d comments=%d files=%d\n",
		len(items), len(shown), countFindings(items), blocks, files)
}

const mdHowTo = "How to fix: the command is under each block, the block id follows the word `block`.\n" +
	"Text is given without `//` and `/* */` - the tool restores the marker, indent\n" +
	"and wrapping itself. Without `--write` a diff prints, the file doesn't change. Lines\n" +
	"shift after the first edit, the block id doesn't. The same text in different files is\n" +
	"one block with one id; the command below edits only its own file.\n" +
	"Looked and decided to leave it: `hum1izer fix --block <id> --keep --write <path>`\n" +
	"adds the block to the baseline, and it won't show up again. Permanently and right\n" +
	"in the code - the word `hum1izer:keep` in the comment text.\n\n"

// deleteOnly - правила, где решать нечего: такой блок удаляется целиком.
// Значение true - удаляется и без модели, это и делает fix --auto.
var deleteOnly = map[string]bool{
	"Commented-out code": true,
	"Empty comment":      true,
	"Separator banner":   false,
}

func deletable(rule, category string) bool {
	_, byRule := deleteOnly[rule]
	_, byCat := deleteOnly[category]
	return byRule || byCat
}

// fixCommand - готовая команда под блок. Слабая модель не связывает хэш из
// шапки с флагом --block, поэтому команда печатается целиком.
func fixCommand(it code.Item) string {
	del := len(it.Findings) > 0
	for _, f := range it.Findings {
		if !deletable(f.Rule, f.Category) {
			del = false
		}
	}
	if del {
		return fmt.Sprintf("```bash\nhum1izer fix --block %s --delete --write %q\n```", it.Hash, it.File)
	}
	return fmt.Sprintf("```bash\nhum1izer fix --block %s --text \"<new text>\" --write %q\n```", it.Hash, it.File)
}

// groupFindings схлопывает одно правило в одну строку со списком строк:
// четыре одинаковых пункта подряд агенту ничего не добавляют.
type findingGroup struct {
	rule, fix string
	lines     []int
	hard      bool
}

func groupFindings(f []code.ItemFinding) []findingGroup {
	var out []findingGroup
	idx := map[string]int{}
	for _, x := range f {
		if i, ok := idx[x.Rule]; ok {
			out[i].lines = append(out[i].lines, x.Line)
			continue
		}
		idx[x.Rule] = len(out)
		out = append(out, findingGroup{rule: x.Rule, fix: x.Fix, lines: []int{x.Line}, hard: x.Hard})
	}
	return out
}

func joinInts(n []int) string {
	parts := make([]string, len(n))
	for i, v := range n {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}

// fenceFor: забор длиннее самой длинной цепочки кавычек внутри текста, иначе
// комментарий с блоком кода развалит разметку.
func fenceFor(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
			continue
		}
		run = 0
	}
	return strings.Repeat("`", max(3, longest+1))
}

func printFindingsJSON(items []code.Item, limit int) {
	out := []code.Finding{}
	for _, it := range cut(items, limit) {
		for _, f := range it.Findings {
			out = append(out, code.Finding{File: it.File, Line: f.Line,
				Rule: f.Rule, Category: f.Category, Lift: f.Lift,
				Fix: f.Fix, Sample: f.Sample, Hard: f.Hard})
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
}

// detail - печатать ли сами находки. На дереве их тысячи, и список правил
// полезнее простыни; на конкретном файле наоборот нужен текст.
func printCodeReport(items []code.Item, files, blocks, limit, top int, detail bool) {
	shown := cut(items, limit)
	byFile := map[string][]code.Item{}
	var order []string
	for _, it := range shown {
		if _, ok := byFile[it.File]; !ok {
			order = append(order, it.File)
		}
		byFile[it.File] = append(byFile[it.File], it)
	}
	sort.Strings(order)

	hard, byRule := 0, map[string]int{}
	for _, it := range items {
		for _, f := range it.Findings {
			byRule[f.Rule]++
			if f.Hard {
				hard++
			}
		}
	}

	for _, file := range order {
		if !detail {
			break
		}
		fmt.Printf("\n=== %s\n", file)
		blocks := byFile[file]
		sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].Start < blocks[j].Start })
		for _, it := range blocks {
			for _, f := range it.Findings {
				mark := "*"
				if f.Hard {
					mark = "!"
				}
				fmt.Printf("  %s:%d  %s %s\n", shortName(it.File), f.Line, mark, f.Rule)
				if f.Sample != "" {
					fmt.Printf("        %s\n", f.Sample)
				}
				if f.Fix != "" {
					fmt.Printf("        -> %s\n", f.Fix)
				}
			}
		}
	}

	fmt.Printf("\n--- total: %d findings (%d hard) in %d blocks, %d comments, %d files\n",
		countFindings(items), hard, len(items), blocks, files)
	if detail && len(shown) < len(items) {
		fmt.Printf("--- shown blocks: %d of %d (--limit)\n", len(shown), len(items))
	}
	if len(byRule) == 0 {
		return
	}
	type kv struct {
		rule string
		n    int
	}
	var list []kv
	for r, n := range byRule {
		list = append(list, kv{r, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].rule < list[j].rule
	})
	if detail && top > 0 && len(list) > top {
		list = list[:top]
	}
	fmt.Println("--- by rule:")
	for _, e := range list {
		fmt.Printf("  %4d  %s\n", e.n, e.rule)
	}
	if !detail {
		fmt.Println("--- findings themselves: name a file or a smaller directory, or use --format md")
	}
}

func shortName(path string) string {
	if strings.HasPrefix(path, "commit ") {
		return path
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
