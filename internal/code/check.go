package code

import (
	"fmt"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"

	"github.com/haiodo/hum1izer/internal/baseline"
	"github.com/haiodo/hum1izer/internal/humanize"
)

// Finding - одна находка в комментарии или коммите, с абсолютным номером строки.
type Finding struct {
	File     string  `json:"file"`
	Category string  `json:"category,omitempty"`
	Line     int     `json:"line"`
	Rule     string  `json:"rule"`
	Fix      string  `json:"fix,omitempty"`
	Sample   string  `json:"sample,omitempty"`
	Lift     float64 `json:"lift,omitempty"`
	Hard     bool    `json:"hard"`
}

// CodeSets - наборы правил для кода: язык выбирается по самому комментарию,
// code применяется всегда.
type CodeSets struct {
	RU, EN, Code *humanize.RuleSet
	MaxLines     int
}

// CheckComment: правила по языку + общие правила комментариев + структурные
// проверки, которых регуляркой не выразить.
func CheckComment(cs CodeSets, c Comment, genre string) []Finding {
	prose := cs.EN
	if humanize.CyrillicShare(c.Text) >= 0.3 {
		prose = cs.RU
	}
	if isLicenseHeader(c) {
		return nil
	}
	var out []Finding
	for _, rs := range []*humanize.RuleSet{prose, cs.Code} {
		if rs == nil {
			continue
		}
		out = append(out, ruleFindings(rs, c, genre)...)
	}
	if genre == "commit" {
		return append(out, commitChecks(c)...)
	}
	return append(out, structChecks(cs.MaxLines, c)...)
}

var (
	spdxRe    = regexp.MustCompile(`SPDX-License-Identifier`)
	licenseRe = regexp.MustCompile(`(?i)copyright|©|licen[cs]ed under|licen[cs]e,? version|` +
		`all rights reserved|под лицензией`)
)

// isLicenseHeader: шапку с лицензией никто не редактирует, и проверять её
// незачем. SPDX считается признаком где угодно, остальное - только в начале
// файла, чтобы не глушить разбор слова "лицензия" в обычном комментарии.
func isLicenseHeader(c Comment) bool {
	if spdxRe.MatchString(c.Text) {
		return true
	}
	return c.Start <= 5 && c.Lines >= 3 && licenseRe.MatchString(c.Text)
}

func ruleFindings(rs *humanize.RuleSet, c Comment, genre string) []Finding {
	words := humanize.CountWords(c.Text)
	bans := humanize.MuteByName(humanize.EffectiveHardBans(rs, humanize.ScanHardBans(rs, c.Text), words), rs.MutedBans(genre))
	soft := humanize.MuteByCategory(humanize.ScanMarkers(rs, c.Text), rs.MutedCategories(genre))
	humanize.FillLines(c.Text, bans)
	humanize.FillLines(c.Text, soft)

	var out []Finding
	for _, group := range []struct {
		hits []humanize.Hit
		hard bool
	}{{bans, true}, {soft, false}} {
		for _, h := range group.hits {
			for _, ln := range h.Lines {
				out = append(out, Finding{
					File: c.File, Category: h.Category, Line: c.Start + ln - 1,
					Rule: h.Marker, Fix: h.Fix, Lift: h.Lift,
					Hard: group.hard, Sample: excerpt(c.Text, ln),
				})
			}
		}
	}
	return out
}

// Категории структурных проверок: по ним их можно отключить целиком.
const (
	StructCategory = "Структура комментария"
	CommitCategory = "Форма коммита"
)

// --- структурные проверки -------------------------------------------------

var (
	bannerRe = regexp.MustCompile(`^\s*[-=*#_~+]{6,}\s*$`)
	todoRe   = regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK|XXX)\b`)
	ownerRe  = regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK|XXX)\b\s*[(\[]|` +
		`(?i)(https?://|#\d+|[A-Z]+-\d+|@[a-z][\w.-]+)`)
	// Ченджлог - это событие с датой или версией. Без них "Update a document"
	// из JSDoc уходит в ложные: там Update это название операции, не история.
	changelogRe = regexp.MustCompile(`(?i)\b(author|created|modified|last updated|дата|автор)\s*:|` +
		`\b(19|20)\d\d[-/.]\d\d[-/.]\d\d\b|` +
		`(?im)^\s*[-*]?\s*(updated?|changed?|added|removed|renamed|fixed|изменено|добавлено|удалено)\b[^\n]{0,60}\b(v\d+(\.\d+)?|\d+\.\d+|\d{4})\b`)
	mdHeadRe   = regexp.MustCompile(`(?m)^\s*#{1,6}\s+\S`)
	mdBulletRe = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d+[.)])\s+\S`)
	stepRe     = regexp.MustCompile(`(?i)(?m)^\s*(?:[-*+]|\d+[.)])?\s*(step|шаг)\s*\d+\s*[:.)]`)
	codeLineRe = regexp.MustCompile(`^\s*(?:if|for|while|switch|return|func|function|const|let|var|import|export|class|struct|guard|public|private|protected|async|await|try|catch|else|case|print|console\.)\b|` +
		`[;{}]\s*$|^\s*[\w.\[\]]+\s*(?::=|=|\+=|-=)\s*\S|^\s*[\w.]+\([^()]*\)\s*[;{]?\s*$`)
	identSplitRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	// ponytail: RE2 без lookahead, поэтому HTTPServer режется как HTTPS+erver.
	// Для сравнения слов комментария с именем этого хватает.
	camelRe = regexp.MustCompile(`[A-Z][a-z]+|[a-z]+|[A-Z]+|\d+`)
)

// structChecks: maxLines - предел длины комментария, 0 отключает проверку.
func structChecks(maxLines int, c Comment) []Finding {
	var out []Finding
	add := func(line int, rule, fix, sample string) {
		out = append(out, Finding{File: c.File, Category: StructCategory,
			Line: line, Rule: rule, Fix: fix, Sample: sample})
	}
	lines := strings.Split(c.Text, "\n")

	if maxLines > 0 && c.Lines > maxLines {
		add(c.Start, "Длинный комментарий",
			fmt.Sprintf("Уложись в %d строки: оставь причину решения, описание вынеси в документацию", maxLines),
			fmt.Sprintf("строк: %d", c.Lines))
	}
	for i, l := range lines {
		if bannerRe.MatchString(l) {
			add(c.Start+i, "Баннер-разделитель", "Разделяй кодом и файлами, а не линиями из символов", strings.TrimSpace(l))
			break
		}
	}
	if why := commentedOutCode(c.Lang, c.Text, lines); why != "" {
		add(c.Start, "Закомментированный код", "Удали: история кода живёт в git, а не в комментарии", why)
	}
	if m := todoRe.FindStringIndex(c.Text); m != nil && !ownerRe.MatchString(c.Text) {
		at := strings.Count(c.Text[:m[0]], "\n")
		add(c.Start+at, "TODO без владельца",
			"Добавь ссылку на задачу или имя: TODO(имя): ... иначе это вечный TODO",
			excerpt(c.Text, at+1))
	}
	if m := changelogRe.FindStringIndex(c.Text); m != nil {
		add(c.Start+strings.Count(c.Text[:m[0]], "\n"), "Ченджлог в комментарии",
			"История правок - в git blame, в коде оставь только текущее положение дел",
			strings.TrimSpace(c.Text[m[0]:minInt(m[1], len(c.Text))]))
	}
	if len(mdHeadRe.FindAllString(c.Text, -1)) > 0 || len(mdBulletRe.FindAllString(c.Text, -1)) >= 3 {
		add(c.Start, "Markdown-эссе в комментарии",
			"Комментарий - это одна мысль рядом с кодом, а не статья с заголовками и списком", "")
	}
	if len(stepRe.FindAllString(c.Text, -1)) >= 2 {
		add(c.Start, "Пошаговая инструкция",
			"Шаги видны в самом коде, комментарий нужен только там, где код их не объясняет", "")
	}
	if w := restatesCode(c); w != "" {
		add(c.Start, "Пересказ кода", "Комментарий повторяет имя ниже - удали или скажи почему, а не что", w)
	}
	return out
}

// proseMarkers: то, что встречается в живом комментарии и почти никогда в коде.
// Без них "// TODO: if x == nil { ... }" уходит в закомментированный код.
var proseMarkers = regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK|XXX|BUG|e\.g\.|i\.e\.|напр\.)\b|https?://`)

// commentedOutCode. Для Go разбираем тело комментария настоящим парсером, как
// это делает go-critic: эвристика по символам ошибается и в обе стороны.
// Для остальных языков штатного парсера в стандартной библиотеке нет, поэтому
// остаётся доля строк, похожих на код.
// ponytail: потолок - TS/Swift/Svelte судятся регуляркой; апгрейд - парсер.
func commentedOutCode(lang, text string, lines []string) string {
	if len(strings.TrimSpace(text)) < 15 || proseMarkers.MatchString(text) {
		return ""
	}
	n, share := codeLikeness(lines)
	if n == 0 {
		return ""
	}
	if lang == "go" {
		if parsesAsGo(text) {
			return fmt.Sprintf("разбирается как код: %d из %d строк", n, len(lines))
		}
		return ""
	}
	if n >= 2 && share >= 0.5 {
		return fmt.Sprintf("%d из %d строк похожи на код", n, len(lines))
	}
	return ""
}

// parsesAsGo: тело комментария подставляется в функцию и отдаётся go/parser.
// Разбор проходит - значит это код, а не проза.
func parsesAsGo(text string) bool {
	src := "package p\nfunc _() {\n" + text + "\n}\n"
	_, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err == nil {
		return true
	}
	// Объявления верхнего уровня в тело функции не лезут, пробуем отдельно.
	_, err = parser.ParseFile(token.NewFileSet(), "", "package p\n"+text+"\n", parser.SkipObjectResolution)
	return err == nil
}

func codeLikeness(lines []string) (int, float64) {
	n, total := 0, 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		total++
		if codeLineRe.MatchString(l) {
			n++
		}
	}
	if total == 0 {
		return 0, 0
	}
	return n, float64(n) / float64(total)
}

// restatesCode: "// set user name" над setUserName() не несёт информации.
// ponytail: только однострочники, смысловой пересказ тут не ловится.
func restatesCode(c Comment) string {
	if c.Lines != 1 || c.Next == "" {
		return ""
	}
	words := significantWords(c.Text)
	if len(words) < 2 || len(words) > 8 {
		return ""
	}
	ident := map[string]bool{}
	for _, tok := range identSplitRe.Split(c.Next, -1) {
		for _, part := range camelRe.FindAllString(tok, -1) {
			ident[strings.ToLower(part)] = true
		}
	}
	hit := 0
	for _, w := range words {
		if ident[w] {
			hit++
		}
	}
	if float64(hit)/float64(len(words)) >= 0.8 {
		return strings.TrimSpace(c.Text)
	}
	return ""
}

// stopWords: служебные слова и глаголы-приставки имён. Без них "// get user name"
// над userName сравнивается по сути, а не по формальному совпадению слов.
var stopWords = humanize.ToSet([]string{"a", "an", "the", "is", "are", "to", "of", "for", "and", "or",
	"this", "that", "it", "in", "on", "by", "we", "will", "function", "method",
	"get", "set", "has", "was", "new", "make", "create", "return", "returns", "check",
	"и", "в", "на", "для", "это", "не", "с", "по", "из", "что",
	"получить", "вернуть", "создать", "проверить", "установить"})

func significantWords(s string) []string {
	var out []string
	for _, w := range humanize.Words(strings.ToLower(s)) {
		if len([]rune(w)) < 2 || stopWords[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

// --- коммиты --------------------------------------------------------------

var (
	genericSubjectRe = regexp.MustCompile(`(?i)^(update|updates|fix|fixes|fixed|changes?|wip|misc|minor|cleanup|refactor|stuff|tmp|temp|test|\.+)\s*$`)
	aiTrailerRe      = regexp.MustCompile(`(?i)co-authored-by:\s*(claude|gpt|copilot|gemini)|generated with \[?claude|\x{1f916} generated`)
	thisCommitRe     = regexp.MustCompile(`(?i)this (commit|change|pr|patch) (adds|introduces|implements|refactors|updates|fixes)`)
)

const subjectLimit = 72

func commitChecks(c Comment) []Finding {
	var out []Finding
	add := func(rule, fix, sample string) {
		out = append(out, Finding{File: c.File, Category: CommitCategory,
			Line: 1, Rule: rule, Fix: fix, Sample: sample})
	}
	subject := c.Text
	if i := strings.IndexByte(subject, '\n'); i >= 0 {
		subject = subject[:i]
	}
	subject = strings.TrimSpace(subject)

	if n := len([]rune(subject)); n > subjectLimit {
		add("Длинный заголовок коммита",
			fmt.Sprintf("Уложись в %d символов, остальное перенеси в тело", subjectLimit),
			fmt.Sprintf("%d символов", n))
	}
	if strings.HasSuffix(subject, ".") {
		add("Точка в конце заголовка", "Заголовок коммита - не предложение, точка в конце лишняя", subject)
	}
	if genericSubjectRe.MatchString(subject) {
		add("Пустой заголовок коммита", "Скажи, что именно изменилось: 'Update' не отличает один коммит от другого", subject)
	}
	if m := aiTrailerRe.FindString(c.Text); m != "" {
		add("AI-подпись в коммите", "Убери трейлер: авторство коммита - твоё", strings.TrimSpace(m))
	}
	if m := thisCommitRe.FindString(c.Text); m != "" {
		add("This commit ...", "Пиши по делу: 'Add retry to upload', а не 'This commit adds ...'", strings.TrimSpace(m))
	}
	if len(mdBulletRe.FindAllString(c.Text, -1)) >= 5 {
		add("Список-простыня в коммите", "Пять и больше пунктов - это пять коммитов", "")
	}
	return out
}

// --- вывод ----------------------------------------------------------------

func excerpt(text string, line int) string {
	lines := strings.Split(text, "\n")
	if line < 1 || line > len(lines) {
		line = 1
	}
	s := strings.TrimSpace(lines[line-1])
	if r := []rune(s); len(r) > 90 {
		s = string(r[:90]) + "..."
	}
	return s
}

// SortFindings: по файлу, внутри файла по строке. Порядок стабилен между
// запусками, иначе diff отчётов бесполезен.
func SortFindings(f []Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		if f[i].File != f[j].File {
			return f[i].File < f[j].File
		}
		if f[i].Line != f[j].Line {
			return f[i].Line < f[j].Line
		}
		return f[i].Rule < f[j].Rule
	})
}

// --- блок для агента ------------------------------------------------------

// Item - блок для агента. Правка идёт по Raw точным совпадением: номера строк
// после первой же правки уезжают.
type Item struct {
	ID       string        `json:"id"`
	Hash     string        `json:"hash"`
	File     string        `json:"file"`
	Start    int           `json:"start"`
	End      int           `json:"end"`
	Lang     string        `json:"lang"`
	Kind     string        `json:"kind"`
	Score    int           `json:"score"`
	Raw      string        `json:"raw"`
	Findings []ItemFinding `json:"findings"`
}

type ItemFinding struct {
	Rule     string  `json:"rule"`
	Category string  `json:"category,omitempty"`
	Fix      string  `json:"fix,omitempty"`
	Line     int     `json:"line"`
	Hard     bool    `json:"hard"`
	Lift     float64 `json:"lift,omitempty"`
	Sample   string  `json:"sample,omitempty"`
}

// NewItem собирает блок. Повторы одного правила на одной строке схлопываются:
// агенту важно правило, а не сколько раз оно совпало.
func NewItem(c Comment, f []Finding) Item {
	kind := "comment"
	if c.Lang == "git" {
		kind = "commit"
	}
	it := Item{ID: c.ID(), Hash: baseline.Hash(c.Text), File: c.File,
		Start: c.Start, End: c.End, Lang: c.Lang, Kind: kind, Raw: c.Raw}
	seen := map[string]bool{}
	for _, x := range f {
		key := fmt.Sprintf("%s:%d", x.Rule, x.Line)
		if seen[key] {
			continue
		}
		seen[key] = true
		it.Findings = append(it.Findings, ItemFinding{
			Rule: x.Rule, Category: x.Category, Fix: x.Fix, Lift: x.Lift,
			Line: x.Line, Hard: x.Hard, Sample: x.Sample})
		it.Score += weight(x)
	}
	return it
}

// weight: вклад находки в Score. lift - измеренное отношение частоты маркера у
// машины к частоте у человека, у неизмеренных правил его нет и вес равен 1.
func weight(f Finding) int {
	w := 1
	if f.Lift > 1 {
		w = min(10, int(f.Lift+0.5))
	}
	if f.Hard {
		w *= 3
	}
	return w
}

// SortItems: сначала самые грязные блоки, чтобы --limit брал их первыми.
func SortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		if items[i].File != items[j].File {
			return items[i].File < items[j].File
		}
		return items[i].Start < items[j].Start
	})
}
