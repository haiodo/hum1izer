// Package config читает .hum1izer.yaml: настройки проекта рядом с кодом.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Name - имя файла настроек. Ищется от каталога проверки вверх до корня.
const Name = ".hum1izer.yaml"

type Comments struct {
	MaxLines  *int  `yaml:"max_lines,omitempty"`
	MaxLine   *int  `yaml:"max_line,omitempty"`
	Commits   *int  `yaml:"commits,omitempty"`
	SkipTests *bool `yaml:"skip_tests,omitempty"`
}

type Languages struct {
	Only   []string `yaml:"only,omitempty"`
	Ignore []string `yaml:"ignore,omitempty"`
}

// Rules: only - белый список, пустой означает все. disable вычитается после.
// Имена берутся из отчёта: подходит и имя правила, и имя категории.
type Rules struct {
	Only    []string `yaml:"only,omitempty"`
	Disable []string `yaml:"disable,omitempty"`
}

type Config struct {
	Version   int       `yaml:"version"`
	Genre     string    `yaml:"genre,omitempty"`
	RulesFile string    `yaml:"rules_file,omitempty"`
	Comments  Comments  `yaml:"comments"`
	Languages Languages `yaml:"languages"`
	Exclude   []string  `yaml:"exclude,omitempty"`
	Baseline  string    `yaml:"baseline,omitempty"`
	Rules     Rules     `yaml:"rules"`

	Path string `yaml:"-"` // откуда прочитан, пусто если настроек нет
}

// Find поднимается от dir вверх, пока не найдёт файл настроек или не упрётся
// в корень. Отсутствие файла не ошибка: возвращается пустая конфигурация.
func Find(dir string) (Config, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Config{}, err
	}
	if st, err := os.Stat(abs); err == nil && !st.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		path := filepath.Join(abs, Name)
		if _, err := os.Stat(path); err == nil {
			return Load(path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Config{}, err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return Config{}, nil
		}
		abs = parent
	}
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true) // опечатка в ключе должна ломать загрузку, а не молчать
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	c.Path = path
	if err := c.validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func (c Config) validate() error {
	for _, l := range append(append([]string{}, c.Languages.Only...), c.Languages.Ignore...) {
		if !KnownLang(l) {
			return fmt.Errorf("неизвестный язык %q, известны: %s", l, strings.Join(Langs(), ", "))
		}
	}
	for _, g := range c.Exclude {
		if _, err := Compile(g); err != nil {
			return fmt.Errorf("exclude %q: %w", g, err)
		}
	}
	if c.Comments.MaxLines != nil && *c.Comments.MaxLines < 0 {
		return errors.New("comments.max_lines не может быть отрицательным")
	}
	if c.Comments.Commits != nil && *c.Comments.Commits < 0 {
		return errors.New("comments.commits не может быть отрицательным")
	}
	return nil
}

// --- языки ----------------------------------------------------------------

// langExt: имя языка в настройках -> расширения файлов.
var langExt = map[string][]string{
	"go":     {".go"},
	"ts":     {".ts", ".tsx"},
	"js":     {".js", ".jsx", ".mjs", ".cjs"},
	"svelte": {".svelte"},
	"swift":  {".swift"},
	"java":   {".java"},
	"kotlin": {".kt", ".kts"},
}

func Langs() []string {
	out := make([]string, 0, len(langExt))
	for l := range langExt {
		out = append(out, l)
	}
	sortStrings(out)
	return out
}

func KnownLang(name string) bool { _, ok := langExt[name]; return ok }

// LangOf возвращает язык по расширению файла, пустую строку для чужого файла.
func LangOf(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	for lang, exts := range langExt {
		for _, e := range exts {
			if e == ext {
				return lang
			}
		}
	}
	return ""
}

// AllowedLangs: nil означает все языки.
func (c Config) AllowedLangs() map[string]bool {
	if len(c.Languages.Only) == 0 && len(c.Languages.Ignore) == 0 {
		return nil
	}
	out := map[string]bool{}
	if len(c.Languages.Only) > 0 {
		for _, l := range c.Languages.Only {
			out[l] = true
		}
	} else {
		for l := range langExt {
			out[l] = true
		}
	}
	for _, l := range c.Languages.Ignore {
		delete(out, l)
	}
	return out
}

// --- исключения -----------------------------------------------------------

// Compile переводит glob в регулярку: ** через каталоги, * внутри одного
// сегмента. Паттерн без слэша сверяется ещё и с именем файла.
func Compile(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	r := []rune(glob)
	for i := 0; i < len(r); i++ {
		switch {
		case r[i] == '*' && i+1 < len(r) && r[i+1] == '*':
			i++
			if i+1 < len(r) && r[i+1] == '/' {
				i++
				b.WriteString("(?:.*/)?")
				continue
			}
			b.WriteString(".*")
		case r[i] == '*':
			b.WriteString("[^/]*")
		case r[i] == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(r[i])))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// Excludes - скомпилированные исключения проекта.
func (c Config) Excludes() ([]*regexp.Regexp, error) {
	var out []*regexp.Regexp
	for _, g := range c.Exclude {
		re, err := Compile(g)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}

// --- правила --------------------------------------------------------------

// Allow: пропускать ли находку с таким именем правила и категории.
func (c Config) Allow(rule, category string) bool {
	if len(c.Rules.Only) > 0 && !hasAny(c.Rules.Only, rule, category) {
		return false
	}
	return !hasAny(c.Rules.Disable, rule, category)
}

func hasAny(list []string, names ...string) bool {
	for _, l := range list {
		for _, n := range names {
			if n != "" && strings.EqualFold(l, n) {
				return true
			}
		}
	}
	return false
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
