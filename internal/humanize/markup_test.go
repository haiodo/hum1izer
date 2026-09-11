package humanize

import (
	"strings"
	"testing"
)

func TestStripMarkupKeepsOffsets(t *testing.T) {
	src := "---\ntitle: Пост\n---\n\nОбычный текст.\n\n```go\nfunc F() { return nil }\n```\n\nЕщё текст `и код` тут.\n"
	got := StripMarkup(src)
	if len(got) != len(src) {
		t.Fatalf("длина изменилась: %d против %d", len(got), len(src))
	}
	if strings.Count(got, "\n") != strings.Count(src, "\n") {
		t.Fatal("число строк изменилось, номера находок уедут")
	}
	for _, s := range []string{"title: Пост", "func F()", "и код"} {
		if strings.Contains(got, s) {
			t.Errorf("не вырезано: %q", s)
		}
	}
	for _, s := range []string{"Обычный текст.", "Ещё текст"} {
		if !strings.Contains(got, s) {
			t.Errorf("проза потерялась: %q", s)
		}
	}
}

func TestStripMarkupFixesRhythm(t *testing.T) {
	code := "```\n" + strings.Repeat("x := someCall(a, b, c)\n", 60) + "```\n"
	src := "Первое предложение. Второе чуть длиннее, с запятой.\n\n" + code + "\nТретье.\n"
	before := rhythm(src)
	after := rhythm(StripMarkup(src))
	if after.MaxLen >= before.MaxLen {
		t.Errorf("блок кода всё ещё считается предложением: было %d, стало %d", before.MaxLen, after.MaxLen)
	}
	if after.Words >= before.Words {
		t.Errorf("слова кода всё ещё считаются: было %d, стало %d", before.Words, after.Words)
	}
}

func TestStripMarkupLinkTextSurvives(t *testing.T) {
	got := StripMarkup("Смотри [подробный разбор](https://example.com/a/b) в конце.")
	if !strings.Contains(got, "подробный разбор") {
		t.Errorf("текст ссылки вырезан: %q", got)
	}
	if strings.Contains(got, "example.com") {
		t.Errorf("адрес ссылки остался: %q", got)
	}
}
