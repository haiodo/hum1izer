package humanize

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

//go:embed rules.yaml
var defaultRules []byte

//go:embed rules-en.yaml
var englishRules []byte

//go:embed rules-code.yaml
var codeRules []byte

// LoadBuiltin: вшитый набор по имени. "ru" - русская проза, "en" - английская,
// "code" - правила для комментариев и коммитов поверх языкового набора.
func LoadBuiltin(name string) (*RuleSet, error) {
	switch name {
	case "ru", "":
		return parseRules(defaultRules)
	case "en":
		return parseRules(englishRules)
	case "code":
		return parseRules(codeRules)
	}
	return nil, fmt.Errorf("нет встроенного набора %q", name)
}

// В Go RE2 нет lookaround и обратных ссылок, а \b и \w работают только по ASCII.
// Поэтому word_re обрамляется границами явно, а три правила считает код.
const (
	notWordStart = `(?:^|[^\p{L}\p{N}_])`
	notWordEnd   = `(?:[^\p{L}\p{N}_]|$)`
)

// --- то, что читается из YAML --------------------------------------------

type Thresholds struct {
	CVHumanTarget    float64 `yaml:"cv_human_target"`
	CVAIThreshold    float64 `yaml:"cv_ai_threshold"`
	NominalTarget    float64 `yaml:"nominal_target"`
	ParaCVAI         float64 `yaml:"para_cv_ai"`
	ParaMinCount     int     `yaml:"para_min_count"`
	ListicleShareAI  float64 `yaml:"listicle_share_ai"`
	ListicleMinItems int     `yaml:"listicle_min_items"`
	BandClean        int     `yaml:"band_clean"`
	BandEdit         int     `yaml:"band_edit"`
}

type RuleSpec struct {
	Name    string `yaml:"name"`
	Fix     string `yaml:"fix"`
	Lit     string `yaml:"lit"`
	Re      string `yaml:"re"`
	WordRe  string `yaml:"word_re"`
	Builtin string `yaml:"builtin"`
}

type CategorySpec struct {
	Name  string     `yaml:"name"`
	Fix   string     `yaml:"fix"`
	Rules []RuleSpec `yaml:"rules"`
}

type GenreSpec struct {
	MuteBans       []string `yaml:"mute_bans"`
	MuteCategories []string `yaml:"mute_categories"`
}

type RuleFile struct {
	Version           int                  `yaml:"version"`
	Thresholds        Thresholds           `yaml:"thresholds"`
	FreqBans          map[string]int       `yaml:"freq_bans"`
	HardBans          []RuleSpec           `yaml:"hard_bans"`
	Categories        []CategorySpec       `yaml:"categories"`
	CopyPasteCategory string               `yaml:"copy_paste_category"`
	Genres            map[string]GenreSpec `yaml:"genres"`
}

// --- скомпилированный набор ----------------------------------------------

type Rule struct {
	Name    string
	Fix     string
	re      *regexp.Regexp
	matcher func(string) []int
}

func (r Rule) find(text string) []int {
	if r.matcher != nil {
		return r.matcher(text)
	}
	var out []int
	for _, m := range r.re.FindAllStringIndex(text, -1) {
		out = append(out, skipLeadingDelim(text, m[0], m[1]))
	}
	return out
}

type Category struct {
	Name  string
	Fix   string
	Rules []Rule
}

type RuleSet struct {
	Thresholds        Thresholds
	FreqBans          map[string]int
	HardBans          []Rule
	Categories        []Category
	CopyPasteCategory string
	Genres            []string
	mutedBans         map[string]map[string]bool
	mutedCategories   map[string]map[string]bool
}

func (rs *RuleSet) HasGenre(name string) bool {
	_, ok := rs.mutedBans[name]
	return ok
}

// MutedBans и MutedCategories - что жанр снимает. Nil для неизвестного жанра:
// ничего не заглушено, это и есть строгий режим.
func (rs *RuleSet) MutedBans(genre string) map[string]bool { return rs.mutedBans[genre] }

func (rs *RuleSet) MutedCategories(genre string) map[string]bool { return rs.mutedCategories[genre] }

// builtins — правила, которые регуляркой не выражаются.
var builtins = map[string]func(string) []int{
	"en_dash":           findEnDash,
	"fake_range":        findFakeRange,
	"latin_in_cyrillic": findLatinInCyrillic,
}

// LoadRules читает набор правил. Пустой path — встроенный rules.yaml.
func LoadRules(path string) (*RuleSet, error) {
	raw := defaultRules
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return parseRules(raw)
}

func parseRules(raw []byte) (*RuleSet, error) {
	var f RuleFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("разбор правил: %w", err)
	}

	rs := &RuleSet{
		Thresholds:        f.Thresholds,
		FreqBans:          f.FreqBans,
		CopyPasteCategory: f.CopyPasteCategory,
		mutedBans:         map[string]map[string]bool{},
		mutedCategories:   map[string]map[string]bool{},
	}
	for i, spec := range f.HardBans {
		r, err := compile(spec, "")
		if err != nil {
			return nil, fmt.Errorf("hard_bans[%d] %q: %w", i, spec.Name, err)
		}
		rs.HardBans = append(rs.HardBans, r)
	}
	for i, cat := range f.Categories {
		c := Category{Name: cat.Name, Fix: cat.Fix}
		for j, spec := range cat.Rules {
			r, err := compile(spec, cat.Fix)
			if err != nil {
				return nil, fmt.Errorf("categories[%d].rules[%d] в %q: %w", i, j, cat.Name, err)
			}
			c.Rules = append(c.Rules, r)
		}
		rs.Categories = append(rs.Categories, c)
	}
	for name, g := range f.Genres {
		rs.Genres = append(rs.Genres, name)
		rs.mutedBans[name] = ToSet(g.MuteBans)
		rs.mutedCategories[name] = ToSet(g.MuteCategories)
	}
	sort.Strings(rs.Genres)
	if err := rs.validate(); err != nil {
		return nil, err
	}
	return rs, nil
}

// validate ловит опечатки в именах: жанр, глушащий несуществующий бан, молча
// не работает, и понять это по выводу невозможно.
func (rs *RuleSet) validate() error {
	if len(rs.HardBans) == 0 && len(rs.Categories) == 0 {
		return fmt.Errorf("в наборе нет ни одного правила")
	}
	known := map[string]bool{}
	for _, b := range rs.HardBans {
		known[b.Name] = true
	}
	for _, c := range rs.Categories {
		known[c.Name] = true
	}
	for genre, muted := range rs.mutedBans {
		for name := range muted {
			if !known[name] {
				return fmt.Errorf("жанр %q глушит неизвестный бан %q", genre, name)
			}
		}
	}
	for genre, muted := range rs.mutedCategories {
		for name := range muted {
			if !known[name] {
				return fmt.Errorf("жанр %q глушит неизвестную категорию %q", genre, name)
			}
		}
	}
	for name := range rs.FreqBans {
		if !known[name] {
			return fmt.Errorf("freq_bans ссылается на неизвестный бан %q", name)
		}
	}
	return nil
}

func compile(spec RuleSpec, catFix string) (Rule, error) {
	r := Rule{Name: spec.Name, Fix: spec.Fix}
	if r.Fix == "" {
		r.Fix = catFix
	}
	set := 0
	for _, s := range []string{spec.Lit, spec.Re, spec.WordRe, spec.Builtin} {
		if s != "" {
			set++
		}
	}
	if set != 1 {
		return r, fmt.Errorf("нужно ровно одно из lit / re / word_re / builtin, задано %d", set)
	}

	switch {
	case spec.Lit != "":
		if r.Name == "" {
			r.Name = spec.Lit
		}
		r.re = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(spec.Lit))
	case spec.Builtin != "":
		fn, ok := builtins[spec.Builtin]
		if !ok {
			return r, fmt.Errorf("неизвестный builtin %q", spec.Builtin)
		}
		r.matcher = fn
	default:
		pattern := spec.Re
		if spec.WordRe != "" {
			pattern = notWordStart + `(?:` + spec.WordRe + `)` + notWordEnd
		}
		re, err := regexp.Compile(`(?i)` + pattern)
		if err != nil {
			return r, err
		}
		r.re = re
	}
	if r.Name == "" {
		return r, fmt.Errorf("у правила нет имени")
	}
	return r, nil
}

func ToSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}

// skipLeadingDelim сдвигает позицию на первую букву совпадения: word_re забирает
// разделитель перед словом, и без сдвига перенос строки уводит находку строкой выше.
func skipLeadingDelim(text string, start, end int) int {
	for i, ch := range text[start:end] {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			return start + i
		}
	}
	return start
}

// --- правила, которые RE2 не выражает ------------------------------------

// findEnDash: короткое тире законно только в числовом диапазоне («2020–2024»).
func findEnDash(text string) []int {
	var out []int
	r := []rune(text)
	offs := runeOffsets(r)
	for i, ch := range r {
		if ch != '–' {
			continue
		}
		if isDigitNear(r, i-1, -1) && isDigitNear(r, i+1, +1) {
			continue
		}
		out = append(out, offs[i])
	}
	return out
}

func isDigitNear(r []rune, pos, step int) bool {
	for pos >= 0 && pos < len(r) {
		switch {
		case r[pos] >= '0' && r[pos] <= '9':
			return true
		case r[pos] == ' ':
			pos += step // допускаем один пробел вокруг тире
		default:
			return false
		}
	}
	return false
}

var fakeRangeRe = regexp.MustCompile(`(?i)` + notWordStart + `от\s+([а-яё]+)\s+до\s+([а-яё]+)` + notWordEnd)

// findFakeRange: «от X до Y» для несвязанных понятий. Исключаем имена
// собственные («от Москвы до Твери») и повтор слова («от зари до зари»).
func findFakeRange(text string) []int {
	var out []int
	for _, m := range fakeRangeRe.FindAllStringSubmatchIndex(text, -1) {
		x, y := text[m[2]:m[3]], text[m[4]:m[5]]
		if strings.EqualFold(x, y) || isCapitalized(x) || isCapitalized(y) {
			continue
		}
		out = append(out, skipLeadingDelim(text, m[0], m[1]))
	}
	return out
}

func isCapitalized(s string) bool {
	for _, ch := range s {
		return unicode.IsUpper(ch)
	}
	return false
}

// findLatinInCyrillic: одиночная латинская буква между кириллическими — подмена
// гомоглифа. Законная латиница кириллицей с двух сторон не окружена.
func findLatinInCyrillic(text string) []int {
	var out []int
	r := []rune(text)
	offs := runeOffsets(r)
	for i := 1; i < len(r)-1; i++ {
		if isLatinLower(r[i]) && isCyrillic(r[i-1]) && isCyrillic(r[i+1]) {
			out = append(out, offs[i])
		}
	}
	return out
}

func runeOffsets(r []rune) []int {
	offs := make([]int, len(r))
	at := 0
	for i, ch := range r {
		offs[i] = at
		at += len(string(ch))
	}
	return offs
}

func isLatinLower(ch rune) bool { return ch >= 'a' && ch <= 'z' }

func isCyrillic(ch rune) bool {
	lo := unicode.ToLower(ch)
	return lo >= 'а' && lo <= 'я' || lo == 'ё'
}
