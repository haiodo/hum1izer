package humanize

import "regexp"

// Разметка и код - не проза. Блок кода без точек становится одним предложением
// на полторы тысячи слов, и ритм после этого не мерится.
var markupParts = []*regexp.Regexp{
	regexp.MustCompile(`(?s)\A---\n.*?\n---\n`),        // YAML-шапка
	regexp.MustCompile("(?ms)^```.*?^```"),             // блок кода
	regexp.MustCompile("(?ms)^~~~.*?^~~~"),             // он же тильдами
	regexp.MustCompile("`[^`\n]*`"),                    // код в строке
	regexp.MustCompile(`(?m)^(?:import|export)\s+.*$`), // MDX-импорты
	regexp.MustCompile(`(?s)<[A-Za-z/][^<>]*>`),        // теги HTML и JSX
	regexp.MustCompile(`\]\([^)\s]+\)`),                // цель ссылки, текст остаётся
	regexp.MustCompile(`(?m)^\s*\|.*\|\s*$`),           // строки таблиц
	regexp.MustCompile(`https?://\S+`),                 // голые ссылки
}

// StripMarkup гасит разметку пробелами: длина и переводы строк сохраняются,
// поэтому номера строк и позиции находок остаются верными.
func StripMarkup(text string) string {
	b := []byte(text)
	for _, re := range markupParts {
		for _, m := range re.FindAllIndex(b, -1) {
			for i := m[0]; i < m[1]; i++ {
				if b[i] != '\n' {
					b[i] = ' '
				}
			}
		}
	}
	return string(b)
}
