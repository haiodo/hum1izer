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

func TestWeightUsesLift(t *testing.T) {
	cases := []struct {
		f    Finding
		want int
	}{
		{Finding{}, 1},
		{Finding{Lift: 9.4}, 9},
		{Finding{Lift: 1.2}, 1},
		{Finding{Lift: 3.1, Hard: true}, 9},
		{Finding{Hard: true}, 3},
		{Finding{Lift: 99}, 10}, // потолок, чтобы одно правило не забирало весь список
	}
	for _, c := range cases {
		if got := weight(c.f); got != c.want {
			t.Errorf("lift=%v hard=%v: вес %d, ожидался %d", c.f.Lift, c.f.Hard, got, c.want)
		}
	}
}

func TestLiftReachesFinding(t *testing.T) {
	cs := CodeSets{MaxLines: 2}
	var err error
	if cs.RU, err = humanize.LoadBuiltin("ru"); err != nil {
		t.Fatal(err)
	}
	if cs.Code, err = humanize.LoadBuiltin("code"); err != nil {
		t.Fatal(err)
	}
	cs.EN = cs.RU
	f := CheckComment(cs, Comment{File: "a.go", Start: 1, Lines: 1,
		Text: "Это не просто кэш, а целый слой"}, "code")
	for _, x := range f {
		if x.Rule == "Не просто X, а Y" {
			if x.Lift < 8 {
				t.Errorf("lift не доехал до находки: %v", x.Lift)
			}
			return
		}
	}
	t.Error("правило с замеренным lift не сработало")
}

func TestCommentedOutCodeGo(t *testing.T) {
	cases := []struct {
		name, text string
		want       bool
	}{
		{"настоящий код", "if err != nil {\n\treturn err\n}", true},
		{"объявление", "func old(a int) int {\n\treturn a * 2\n}", true},
		{"проза со скобками", "счётчик сбрасывается, когда очередь пустеет (см. reset)", false},
		{"TODO рядом с кодом", "TODO: вернуть return err после рефакторинга", false},
		{"ссылка", "формат описан в https://example.com/spec, поле id = число", false},
		{"короткий", "x := 1", false},
	}
	for _, c := range cases {
		got := commentedOutCode("go", c.text, strings.Split(c.text, "\n")) != ""
		if got != c.want {
			t.Errorf("%s: %v, ожидалось %v", c.name, got, c.want)
		}
	}
}

func TestTrailingCommentIsSeparate(t *testing.T) {
	src := "package a\n\n// шапка блока\nfunc f() {\n\tx := 1 // счётчик, а не размер\n\t_ = x\n}\n"
	got := extract(t, "a.go", src)
	if len(got) != 2 {
		t.Fatalf("блоков: %d, ожидалось 2: %q", len(got), texts(got))
	}
	tail := got[1]
	if strings.Contains(tail.Raw, "x := 1") {
		t.Errorf("в raw хвостового комментария попал код: %q", tail.Raw)
	}
	if tail.Raw != "// счётчик, а не размер" {
		t.Errorf("raw = %q", tail.Raw)
	}
	if tail.Text != "счётчик, а не размер" {
		t.Errorf("text = %q", tail.Text)
	}
}

func TestTrailingCommentSeesItsOwnLine(t *testing.T) {
	src := "package a\n\nfunc f() {\n\tuserName := req.Name // get user name\n\t_ = userName\n}\n"
	got := extract(t, "a.go", src)
	if len(got) != 1 {
		t.Fatalf("блоков: %d", len(got))
	}
	if got[0].Next != "userName := req.Name" {
		t.Fatalf("next = %q", got[0].Next)
	}
	if restatesCode(got[0]) == "" {
		t.Error("пересказ в хвостовом комментарии не пойман")
	}
}

func TestTrailingCommentNotMergedWithNext(t *testing.T) {
	src := "package a\n\nfunc f() {\n\tx := 1 // хвост\n\t// своя строка\n\t_ = x\n}\n"
	got := extract(t, "a.go", src)
	if len(got) != 2 {
		t.Fatalf("хвостовой склеился со следующим: %q", texts(got))
	}
}

func TestRawKeepsMarkers(t *testing.T) {
	src := "package a\n\n// раз\n// два\nfunc f() {}\n"
	got := extract(t, "a.go", src)
	if len(got) != 1 || got[0].Raw != "// раз\n// два" {
		t.Fatalf("raw = %q", got[0].Raw)
	}
}
