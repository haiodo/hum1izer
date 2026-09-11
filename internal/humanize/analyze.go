package humanize

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

// Hit — попадание одного правила.
type Hit struct {
	Category  string `json:"category"`
	Marker    string `json:"marker"`
	Fix       string `json:"fix,omitempty"`
	Count     int    `json:"count"`
	Positions []int  `json:"-"`
	Lines     []int  `json:"lines"`
}

// Rhythm — ритм предложений и типографика.
type Rhythm struct {
	Sentences   int     `json:"sentences"`
	Words       int     `json:"words"`
	MeanLen     float64 `json:"mean_len"`
	CVLen       float64 `json:"cv_len"`
	MinLen      int     `json:"min_len"`
	MaxLen      int     `json:"max_len"`
	EmDash      int     `json:"em_dash"`
	Ellipsis    int     `json:"ellipsis"`
	Parentheses int     `json:"parentheses"`
	Questions   int     `json:"questions"`
}

// Morph — номинальность. ponytail: суффиксная эвристика вместо pymorphy3, не
// отличает «решение задачи» от «решение принято»; апгрейд — морфоанализатор.
type Morph struct {
	Nominalizations int     `json:"nominalizations"`
	Per100          float64 `json:"per_100_words"`
	VerbForms       int     `json:"verb_forms"`
}

// Structure — метрики уровня документа.
type Structure struct {
	Paragraphs      int     `json:"paragraphs"`
	ParaMeanSent    float64 `json:"para_mean_sent"`
	ParaCV          float64 `json:"para_cv"`
	ListItems       int     `json:"list_items"`
	ListicleShare   float64 `json:"listicle_share"`
	TitleCaseHeads  int     `json:"title_case_headings"`
	TruncatedEnding bool    `json:"truncated_ending"`
}

// Report — всё, что посчитано по тексту.
type Report struct {
	Genre      string    `json:"genre"`
	HardBans   []Hit     `json:"hard_bans"`
	Markers    []Hit     `json:"markers"`
	Rhythm     Rhythm    `json:"rhythm"`
	Morph      Morph     `json:"morph"`
	Structure  Structure `json:"structure"`
	Score      Score     `json:"score"`
	MutedBans  int       `json:"muted_bans"`
	MutedSoft  int       `json:"muted_markers"`
	NotRussian bool      `json:"not_russian"`
}

func Analyze(rs *RuleSet, text, genre string) Report {
	r := Report{Genre: genre}
	r.Rhythm = rhythm(text)
	r.Morph = morph(text, r.Rhythm.Words)
	r.Structure = structure(text)
	r.NotRussian = CyrillicShare(text) < 0.3

	allBans := ScanHardBans(rs, text)
	effective := EffectiveHardBans(rs, allBans, r.Rhythm.Words)
	r.HardBans = MuteByName(effective, rs.MutedBans(genre))
	r.MutedBans = len(effective) - len(r.HardBans)

	allSoft := ScanMarkers(rs, text)
	r.Markers = MuteByCategory(allSoft, rs.MutedCategories(genre))
	r.MutedSoft = len(allSoft) - len(r.Markers)

	dashMuted := rs.MutedBans(genre)["Длинное тире"]
	r.Score = cleanliness(rs, r, dashMuted)
	FillLines(text, r.HardBans)
	FillLines(text, r.Markers)
	return r
}

func ScanHardBans(rs *RuleSet, text string) []Hit {
	var hits []Hit
	for _, rule := range rs.HardBans {
		if pos := rule.find(text); len(pos) > 0 {
			hits = append(hits, Hit{Category: "HARD BAN", Marker: rule.Name,
				Fix: rule.Fix, Count: len(pos), Positions: pos})
		}
	}
	return hits
}

func ScanMarkers(rs *RuleSet, text string) []Hit {
	var hits []Hit
	for _, cat := range rs.Categories {
		for _, rule := range cat.Rules {
			if pos := rule.find(text); len(pos) > 0 {
				hits = append(hits, Hit{Category: cat.Name, Marker: rule.Name,
					Fix: cat.Fix, Count: len(pos), Positions: pos})
			}
		}
	}
	return hits
}

// EffectiveHardBans: частотные баны остаются банами только выше плотности 1/N слов.
func EffectiveHardBans(rs *RuleSet, hits []Hit, words int) []Hit {
	out := hits[:0:0]
	for _, h := range hits {
		if per, ok := rs.FreqBans[h.Marker]; ok {
			if h.Count < 2 || float64(h.Count) <= float64(words)/float64(per) {
				continue
			}
		}
		out = append(out, h)
	}
	return out
}

func MuteByName(hits []Hit, muted map[string]bool) []Hit {
	out := hits[:0:0]
	for _, h := range hits {
		if !muted[h.Marker] {
			out = append(out, h)
		}
	}
	return out
}

func MuteByCategory(hits []Hit, muted map[string]bool) []Hit {
	out := hits[:0:0]
	for _, h := range hits {
		if !muted[h.Category] {
			out = append(out, h)
		}
	}
	return out
}

func FillLines(text string, hits []Hit) {
	for i := range hits {
		seen := map[int]bool{}
		for _, p := range hits[i].Positions {
			line := strings.Count(text[:p], "\n") + 1
			if !seen[line] {
				seen[line] = true
				hits[i].Lines = append(hits[i].Lines, line)
			}
		}
	}
}

// --- ритм -----------------------------------------------------------------

// sentenceEnd: конец предложения — .!?… перед пробелом с заглавной или концом строки.
// Замена razdel.sentenize: сокращения вроде «т.д.» она режет точнее, нам хватает этого.
var sentenceEnd = regexp.MustCompile(`[.!?…]+[)"»']*\s`)

func splitSentences(text string) []string {
	var out []string
	start := 0
	for _, m := range sentenceEnd.FindAllStringIndex(text, -1) {
		s := strings.TrimSpace(text[start:m[1]])
		if s != "" {
			out = append(out, s)
		}
		start = m[1]
	}
	if s := strings.TrimSpace(text[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}]+`)

func CountWords(s string) int { return len(wordRe.FindAllString(s, -1)) }

// Words - слова текста, буквы и цифры. Нужен пакету code, чтобы сравнивать
// слова комментария с именем в коде тем же способом, что и тут.
func Words(s string) []string { return wordRe.FindAllString(s, -1) }

func rhythm(text string) Rhythm {
	var lens []int
	sents := splitSentences(text)
	questions := 0
	for _, s := range sents {
		if n := CountWords(s); n > 0 {
			lens = append(lens, n)
			if strings.HasSuffix(strings.TrimSpace(s), "?") {
				questions++
			}
		}
	}
	r := Rhythm{
		Sentences:   len(lens),
		EmDash:      strings.Count(text, "—"),
		Ellipsis:    strings.Count(text, "…") + strings.Count(text, "..."),
		Parentheses: strings.Count(text, "("),
		Questions:   questions,
	}
	if len(lens) == 0 {
		return r
	}
	sum, min, max := 0, lens[0], lens[0]
	for _, n := range lens {
		sum += n
		if n < min {
			min = n
		}
		if n > max {
			max = n
		}
	}
	r.Words, r.MinLen, r.MaxLen = sum, min, max
	mean := float64(sum) / float64(len(lens))
	r.MeanLen = round(mean, 1)
	if len(lens) > 1 && mean > 0 {
		variance := 0.0
		for _, n := range lens {
			d := float64(n) - mean
			variance += d * d
		}
		r.CVLen = round(math.Sqrt(variance/float64(len(lens)))/mean, 3)
	}
	return r
}

// --- морфология -----------------------------------------------------------

var nominalSuffixes = []string{"ение", "ания", "ения", "ание", "ация", "изация",
	"ировка", "ость", "остей", "ений", "аний"}

// verbSuffixes: грубый признак глагольной формы. Причастия сюда попадают
// намеренно, но «мать» и «путь» дают ложное «ть».
var verbSuffixes = []string{"ться", "тся", "ешь", "ишь", "ете", "ите", "ают", "яют",
	"уют", "юют", "ует", "ирует", "ают", "ял", "ил", "ал", "ла", "ли", "ло", "вший", "вшая"}

func morph(text string, words int) Morph {
	m := Morph{}
	for _, w := range wordRe.FindAllString(strings.ToLower(text), -1) {
		if len([]rune(w)) < 5 {
			continue
		}
		if hasAnySuffix(w, nominalSuffixes) {
			m.Nominalizations++
			continue
		}
		if hasAnySuffix(w, verbSuffixes) {
			m.VerbForms++
		}
	}
	if words > 0 {
		m.Per100 = round(float64(m.Nominalizations)/float64(words)*100, 2)
	}
	return m
}

func hasAnySuffix(w string, suffixes []string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(w, s) {
			return true
		}
	}
	return false
}

// --- структура ------------------------------------------------------------

var (
	listRe      = regexp.MustCompile(`^\s*(?:[-*•]|\d+[.)])\s+\S`)
	paraSplitRe = regexp.MustCompile(`\n\s*\n`)
	cyrWordRe   = regexp.MustCompile(`[А-Яа-яЁё][А-Яа-яЁё-]*`)
)

var headingStopwords = ToSet([]string{"и", "в", "на", "с", "к", "по", "для", "из", "о", "об",
	"а", "но", "или", "у", "за"})

func structure(text string) Structure {
	var sentCounts []int
	for _, p := range paraSplitRe.Split(text, -1) {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if n := len(splitSentences(p)); n > 0 {
			sentCounts = append(sentCounts, n)
		}
	}
	s := Structure{Paragraphs: len(sentCounts)}
	if len(sentCounts) > 0 {
		sum := 0
		for _, n := range sentCounts {
			sum += n
		}
		mean := float64(sum) / float64(len(sentCounts))
		s.ParaMeanSent = round(mean, 1)
		if len(sentCounts) > 1 && mean > 0 {
			variance := 0.0
			for _, n := range sentCounts {
				d := float64(n) - mean
				variance += d * d
			}
			s.ParaCV = round(math.Sqrt(variance/float64(len(sentCounts)))/mean, 3)
		}
	}

	var lines []string
	for _, ln := range strings.Split(text, "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	for _, ln := range lines {
		if listRe.MatchString(ln) {
			s.ListItems++
		}
	}
	if len(lines) > 0 {
		s.ListicleShare = round(float64(s.ListItems)/float64(len(lines)), 3)
	}
	s.TitleCaseHeads = countTitleCaseHeadings(text)
	s.TruncatedEnding = isTruncated(lines)
	return s
}

// countTitleCaseHeadings: «Ранняя Жизнь и Образование» — калька с английского.
// ponytail: без словаря имён заголовок «Иван Петров и Пётр Иванов» даст ложное.
func countTitleCaseHeadings(text string) int {
	lines := strings.Split(text, "\n")
	hits := 0
	for i, raw := range lines {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		isMD := strings.HasPrefix(s, "#")
		nextEmpty := i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) == ""
		looksLike := len([]rune(s)) <= 80 && !strings.ContainsAny(s[len(s)-1:], ".!?:;,") &&
			nextEmpty && !listRe.MatchString(raw)
		if !isMD && !looksLike {
			continue
		}
		words := cyrWordRe.FindAllString(s, -1)
		if len(words) < 3 {
			continue
		}
		capped := 0
		for _, w := range words[1:] {
			r := []rune(w)
			if unicode.IsUpper(r[0]) && !headingStopwords[strings.ToLower(w)] {
				capped++
			}
		}
		if capped >= 2 {
			hits++
		}
	}
	return hits
}

// isTruncated: последняя содержательная строка прозы обязана кончаться знаком
// конца предложения. Иначе модель упёрлась в лимит токенов.
func isTruncated(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	last := strings.TrimRight(lines[len(lines)-1], " \t")
	if strings.HasPrefix(last, "#") || listRe.MatchString(last) ||
		strings.HasPrefix(last, "|") || strings.HasPrefix(last, ">") ||
		strings.HasPrefix(last, "```") {
		return false
	}
	if len(cyrWordRe.FindAllString(last, -1)) < 4 {
		return false
	}
	return !strings.ContainsAny(last[len(last)-1:], `.!?…:»)"'*_`)
}

func CyrillicShare(text string) float64 {
	letters, cyr := 0, 0
	for _, ch := range text {
		if !unicode.IsLetter(ch) {
			continue
		}
		letters++
		if isCyrillic(ch) {
			cyr++
		}
	}
	if letters == 0 {
		return 0
	}
	return float64(cyr) / float64(letters)
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}
