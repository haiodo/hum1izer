package code

import (
	"github.com/haiodo/hum1izer/internal/humanize"

	"os"
	"path/filepath"
	"strings"
	"testing"
)

func extract(t *testing.T, name, body string) []Comment {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ExtractComments(p)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return got
}

func texts(cs []Comment) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Text
	}
	return out
}

func TestExtractGo(t *testing.T) {
	src := "package a\n\n// первая строка\n// вторая строка\nfunc f() {\n\ts := \"не // комментарий\"\n\t_ = s\n\t/* блок\n\t   продолжение */\n}\n"
	got := extract(t, "a.go", src)
	if len(got) != 2 {
		t.Fatalf("блоков: %d, ожидалось 2: %q", len(got), texts(got))
	}
	if got[0].Text != "первая строка\nвторая строка" || got[0].Start != 3 || got[0].Lines != 2 {
		t.Errorf("первый блок: %+v", got[0])
	}
	if !strings.HasPrefix(got[1].Text, "блок") {
		t.Errorf("блочный комментарий: %q", got[1].Text)
	}
}

func TestExtractTSIgnoresLiterals(t *testing.T) {
	src := "const re = /https:\\/\\/x/;\nconst s = `url // not a comment`;\n// настоящий комментарий\nconst a = 1;\n"
	got := extract(t, "a.ts", src)
	if len(got) != 1 || got[0].Text != "настоящий комментарий" {
		t.Fatalf("ожидался один комментарий, получено %q", texts(got))
	}
	if got[0].Start != 3 {
		t.Errorf("строка: %d, ожидалось 3", got[0].Start)
	}
}

func TestExtractSvelte(t *testing.T) {
	src := "<script lang=\"ts\">\n  // внутри скрипта\n  let a = 1;\n</script>\n\n<!-- в разметке -->\n<div>{a}</div>\n"
	got := extract(t, "a.svelte", src)
	if len(got) != 2 {
		t.Fatalf("блоков: %d, ожидалось 2: %q", len(got), texts(got))
	}
}

func TestExtractSwift(t *testing.T) {
	src := "/// документация\nfunc f() {\n    let s = \"// не комментарий\"\n}\n"
	got := extract(t, "a.swift", src)
	if len(got) != 1 || got[0].Text != "документация" || !got[0].Doc {
		t.Fatalf("свифт: %+v", got)
	}
}

func TestRestatesCode(t *testing.T) {
	yes := Comment{Lines: 1, Text: "set user name", Next: "func setUserName(n string) {"}
	if restatesCode(yes) == "" {
		t.Error("пересказ имени функции не пойман")
	}
	no := Comment{Lines: 1, Text: "имя хранится в нижнем регистре, база регистрозависима",
		Next: "func setUserName(n string) {"}
	if restatesCode(no) != "" {
		t.Error("осмысленный комментарий принят за пересказ")
	}
	// Глагол-приставка в счёт не идёт: "get user name" над userName - пересказ.
	if restatesCode(Comment{Lines: 1, Text: "get user name", Next: "const userName = req.name;"}) == "" {
		t.Error("пересказ имени переменной не пойман")
	}
	if restatesCode(Comment{Lines: 1, Text: "get user name from cache", Next: "const userName = req.name;"}) != "" {
		t.Error("комментарий с новой информацией принят за пересказ")
	}
}

func TestStructChecks(t *testing.T) {
	cases := []struct {
		name string
		c    Comment
		want string
	}{
		{"баннер", Comment{Lines: 1, Text: "========================"}, "Баннер-разделитель"},
		{"код", Comment{Lines: 3, Text: "if (x) {\n  doIt();\n}"}, "Закомментированный код"},
		{"todo", Comment{Lines: 1, Text: "TODO: починить"}, "TODO без владельца"},
		{"ченджлог", Comment{Lines: 1, Text: "Updated parser to support v2"}, "Ченджлог в комментарии"},
		{"эссе", Comment{Lines: 3, Text: "## Как это работает\n\nтекст"}, "Markdown-эссе в комментарии"},
		{"шаги", Comment{Lines: 2, Text: "- Step 1: validate\n- Step 2: persist"}, "Пошаговая инструкция"},
	}
	for _, c := range cases {
		if !hasRule(structChecks(2, c.c), c.want) {
			t.Errorf("%s: правило %q не сработало", c.name, c.want)
		}
	}
	// TODO со ссылкой на задачу - законный.
	if hasRule(structChecks(2, Comment{Lines: 1, Text: "TODO(haiodo): починить"}), "TODO без владельца") {
		t.Error("TODO с владельцем не должен считаться находкой")
	}
}

func TestCommitChecks(t *testing.T) {
	long := strings.Repeat("длинный заголовок ", 6)
	cases := []struct{ text, want string }{
		{long, "Длинный заголовок коммита"},
		{"Добавить ретраи.", "Точка в конце заголовка"},
		{"Update", "Пустой заголовок коммита"},
		{"Fix retry\n\nCo-Authored-By: Claude <noreply@anthropic.com>", "AI-подпись в коммите"},
		{"Add retry\n\nThis commit adds retry logic", "This commit ..."},
	}
	for _, c := range cases {
		if !hasRule(commitChecks(Comment{Text: c.text}), c.want) {
			t.Errorf("%q: правило %q не сработало", c.text, c.want)
		}
	}
	if f := commitChecks(Comment{Text: "Add retry to upload"}); len(f) != 0 {
		t.Errorf("нормальный коммит дал находки: %+v", f)
	}
}

func TestCheckCommentPicksLanguage(t *testing.T) {
	cs := CodeSets{MaxLines: 2}
	var err error
	if cs.RU, err = humanize.LoadBuiltin("ru"); err != nil {
		t.Fatal(err)
	}
	if cs.EN, err = humanize.LoadBuiltin("en"); err != nil {
		t.Fatal(err)
	}
	if cs.Code, err = humanize.LoadBuiltin("code"); err != nil {
		t.Fatal(err)
	}
	ru := CheckComment(cs, Comment{File: "a.go", Start: 10, Lines: 1,
		Text: "Данный метод отвечает за обработку"}, "code")
	if len(ru) == 0 {
		t.Error("русский комментарий-пустышка не пойман")
	}
	for _, f := range ru {
		if f.Line != 10 {
			t.Errorf("строка %d, ожидалась 10 (%s)", f.Line, f.Rule)
		}
	}
	en := CheckComment(cs, Comment{File: "a.ts", Start: 1, Lines: 1,
		Text: "This function is responsible for handling the request"}, "code")
	if len(en) == 0 {
		t.Error("английский комментарий-пустышка не пойман")
	}
}

func hasRule(f []Finding, rule string) bool {
	for _, x := range f {
		if x.Rule == rule {
			return true
		}
	}
	return false
}
