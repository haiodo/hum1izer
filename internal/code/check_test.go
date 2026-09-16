package code

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haiodo/hum1izer/internal/humanize"
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

func TestExtractSvelteClosingTagInString(t *testing.T) {
	src := "<script>\n  const s = \"</style>\";\n  // комментарий после литерала\n</script>\n" +
		"<style>\n  /* в стилях */\n</style>\n"
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
		{"баннер", Comment{Lines: 1, Text: "========================"}, "Separator banner"},
		{"код", Comment{Lines: 3, Text: "if (x) {\n  doIt();\n}"}, "Commented-out code"},
		{"todo", Comment{Lines: 1, Text: "TODO: починить"}, "TODO with no owner"},
		{"ченджлог", Comment{Lines: 1, Text: "Updated parser to support v2"}, "Changelog in a comment"},
		{"автор", Comment{Lines: 1, Text: "Author: кто-то"}, "Changelog in a comment"},
		{"эссе", Comment{Lines: 3, Text: "## Как это работает\n\nтекст"}, "Markdown essay in a comment"},
		{"шаги", Comment{Lines: 2, Text: "- Step 1: validate\n- Step 2: persist"}, "Step-by-step instructions"},
	}
	for _, c := range cases {
		if !hasRule(structChecks(2, 0, c.c), c.want) {
			t.Errorf("%s: правило %q не сработало", c.name, c.want)
		}
	}
	// Описание операции в JSDoc - не ченджлог.
	if hasRule(structChecks(12, 0, Comment{Lines: 1, Text: "Update a top-level document by id"}), "Changelog in a comment") {
		t.Error("описание операции принято за ченджлог")
	}
	// Обычное слово "ToDo" - не маркер.
	for _, text := range []string{
		"Todos are resolved per batch and dropped with it",
		"A helper class to control classic project todo automation",
		"The separator between the todo list and the calendar",
	} {
		if hasRule(structChecks(12, 0, Comment{Lines: 1, Text: text}), "TODO with no owner") {
			t.Errorf("обычное слово принято за маркер: %q", text)
		}
	}
	// Маркер в кавычках - цитата, а не задача.
	if hasRule(structChecks(12, 0, Comment{Lines: 1, Text: `пример: "// TODO: fix" уходит в код`}), "TODO with no owner") {
		t.Error("маркер в кавычках принят за задачу")
	}
	// Строчный маркер со знаком после - настоящий.
	if !hasRule(structChecks(12, 0, Comment{Lines: 1, Text: "todo: починить"}), "TODO with no owner") {
		t.Error("строчный todo: не пойман")
	}
	// TODO со ссылкой на задачу - законный.
	if hasRule(structChecks(2, 0, Comment{Lines: 1, Text: "TODO(haiodo): починить"}), "TODO with no owner") {
		t.Error("TODO с владельцем не должен считаться находкой")
	}
}

func TestCommitChecks(t *testing.T) {
	long := strings.Repeat("длинный заголовок ", 6)
	cases := []struct{ text, want string }{
		{long, "Long commit subject"},
		{"Добавить ретраи.", "Period at the end of the subject"},
		{"Update", "Empty commit subject"},
		{"Fix retry\n\nCo-Authored-By: Claude <noreply@anthropic.com>", "AI trailer in the commit"},
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
	cs := CodeSets{MaxLines: 2, MaxLineLen: 100}
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
	cs := CodeSets{MaxLines: 2, MaxLineLen: 100}
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

func TestLicenseHeaderSkipped(t *testing.T) {
	lic := Comment{Start: 1, Lines: 4, Text: "Copyright (c) 2026 Someone\n\nLicensed under the Apache License, Version 2.0\nyou may not use this file except in compliance"}
	if !isLicenseHeader(lic) {
		t.Error("лицензионная шапка не распознана")
	}
	if !isLicenseHeader(Comment{Start: 40, Lines: 1, Text: "SPDX-License-Identifier: MIT"}) {
		t.Error("SPDX не распознан вне начала файла")
	}
	// Обычный комментарий про лицензии в середине файла глушить нельзя.
	if isLicenseHeader(Comment{Start: 120, Lines: 3, Text: "тут проверяем copyright у загруженного файла\nи пишем в лог\nесли его нет"}) {
		t.Error("обычный комментарий принят за лицензионную шапку")
	}
}

func TestWalkRespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git недоступен: %v %s", err, out)
		}
	}
	run("init", "-q")
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "lib/\n")
	write("src/a.ts", "// комментарий\nexport const a = 1;\n")
	write("lib/a.js", "// собранное\nexport const a = 1;\n")

	got, err := WalkCode(dir, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "src/a.ts") {
		t.Errorf("исходник не найден: %v", got)
	}
	if strings.Contains(joined, "lib/a.js") {
		t.Errorf("игнорируемый файл попал в обход: %v", got)
	}
}

func TestPackageDocNotTooLong(t *testing.T) {
	doc := Comment{Lines: 6, Text: "Package code разбирает комментарии.\nПодробности тут.\nИ ещё строка.",
		Next: "package code"}
	if hasRule(structChecks(2, 0, doc), "Long comment") {
		t.Error("пакетная документация оштрафована за длину")
	}
	inline := doc
	inline.Next = "func f() {"
	if !hasRule(structChecks(2, 0, inline), "Long comment") {
		t.Error("обычный длинный комментарий пропущен")
	}
}

func TestLongCommentLine(t *testing.T) {
	long := "// " + strings.Repeat("слово ", 30)
	f := structChecks(2, 100, Comment{Lines: 1, Text: long})
	if !hasRule(f, "Long comment line") {
		t.Error("склеенная в одну длинную строку простыня не поймана")
	}
	if hasRule(structChecks(2, 100, Comment{Lines: 1, Text: "короткая строка"}), "Long comment line") {
		t.Error("короткая строка принята за длинную")
	}
}

func TestCommentedOutCodeTS(t *testing.T) {
	cases := []struct {
		name, lang, text string
		want             bool
	}{
		{"настоящий код", "ts", "if (err) {\n  return null;\n}", true},
		{"типы TypeScript", "ts", "export function f(a: number): string { return String(a) }", true},
		{"вызов с объектом", "js", "logger.info(\"ros2: publishing\", { id: cfg.id });", true},
		{"проза со скобками", "ts", "счётчик сбрасывается, когда очередь пустеет (см. reset)", false},
		{"английская проза", "ts", "the query fires on every update to this item, due date included", false},
		{"проза с дефисом", "ts", "Normalizes audio samples to target RMS while preserving dynamics", false},
	}
	for _, c := range cases {
		got := commentedOutCode(c.lang, c.text, strings.Split(c.text, "\n")) != ""
		if got != c.want {
			t.Errorf("%s (%s): %v, ожидалось %v", c.name, c.lang, got, c.want)
		}
	}
}

func TestExtractJava(t *testing.T) {
	src := "class A {\n" +
		"    // обычный комментарий\n" +
		"    String s = \"\"\"\n        // не комментарий, это текстовый блок\n        \"\"\";\n" +
		"    int x = 1; // хвостовой\n" +
		"}\n"
	got := extract(t, "A.java", src)
	if len(got) != 2 {
		t.Fatalf("блоков: %d, ожидалось 2: %q", len(got), texts(got))
	}
	if got[0].Text != "обычный комментарий" {
		t.Errorf("первый блок: %q", got[0].Text)
	}
	if got[1].Text != "хвостовой" {
		t.Errorf("второй блок: %q", got[1].Text)
	}
}

func TestExtractKotlin(t *testing.T) {
	src := "fun f() {\n" +
		"    /* внешний /* вложенный */ всё ещё комментарий */\n" +
		"    val s = \"\"\"// не комментарий\"\"\"\n" +
		"    val c = '\\''  // хвост после символа\n" +
		"}\n"
	got := extract(t, "a.kt", src)
	if len(got) != 2 {
		t.Fatalf("блоков: %d, ожидалось 2: %q", len(got), texts(got))
	}
	if !strings.Contains(got[0].Text, "всё ещё комментарий") {
		t.Errorf("вложенный блок закрылся рано: %q", got[0].Text)
	}
	if got[1].Text != "хвост после символа" {
		t.Errorf("второй блок: %q", got[1].Text)
	}
}

func TestKotlinLangDetected(t *testing.T) {
	for name, want := range map[string]string{"a.kt": "kotlin", "a.kts": "kotlin", "A.java": "java"} {
		got := extract(t, name, "// комментарий\nclass A {}\n")
		if len(got) == 0 || got[0].Lang != want {
			t.Errorf("%s: язык %q, ожидался %q", name, got[0].Lang, want)
		}
	}
}

func TestDocLeadKeepsGodocConvention(t *testing.T) {
	cs := CodeSets{MaxLines: 2, MaxLineLen: 100}
	var err error
	if cs.EN, err = humanize.LoadBuiltin("en"); err != nil {
		t.Fatal(err)
	}
	if cs.Code, err = humanize.LoadBuiltin("code"); err != nil {
		t.Fatal(err)
	}

	godoc := CheckComment(cs, Comment{File: "a.go", Start: 1, Lines: 1,
		Text: "Chunk represents a chunk of raw data", Next: "type Chunk struct {"}, "code")
	if hasRule(godoc, "represents a/an") {
		t.Error("godoc-конвенция поймана как копула")
	}

	jsdoc := CheckComment(cs, Comment{File: "a.ts", Start: 1, Lines: 1, Doc: true,
		Text: "Represents a relationship between two rows"}, "code")
	if hasRule(jsdoc, "represents a/an") {
		t.Error("JSDoc-конвенция поймана как копула")
	}

	// В коде вся категория заглушена замером на ядре, поэтому проверка guard'а
	// идёт в прозаическом жанре: там копула по-прежнему ловится.
	lead := CheckComment(cs, Comment{File: "a.go", Start: 1, Lines: 1, Doc: true,
		Text: "Chunk represents a chunk of raw data"}, "marketing")
	if hasRule(lead, "represents a/an") {
		t.Error("ведущая строка doc-комментария поймана как копула")
	}
	prose := CheckComment(cs, Comment{File: "a.go", Start: 1, Lines: 1,
		Text: "This represents a shift in how we handle retries", Next: "x := 1"}, "marketing")
	if !hasRule(prose, "represents a/an") {
		t.Error("копула в обычном комментарии пропущена")
	}
	inCode := CheckComment(cs, Comment{File: "a.go", Start: 1, Lines: 1,
		Text: "This represents a shift in how we handle retries", Next: "x := 1"}, "code")
	if hasRule(inCode, "represents a/an") {
		t.Error("копула в коде должна быть заглушена жанром")
	}
}

func TestLengthCountsProseOnly(t *testing.T) {
	jsdoc := Comment{Lines: 5, Text: "Sends a message.\n@param to - recipient\n@param body - text\n@returns id", Next: "function send() {"}
	if hasRule(structChecks(2, 100, jsdoc), "Long comment") {
		t.Error("блок из тегов JSDoc оштрафован за длину")
	}
	prose := Comment{Lines: 4, Text: "Первая строка.\nВторая строка.\nТретья строка.", Next: "func f() {"}
	if !hasRule(structChecks(2, 100, prose), "Long comment") {
		t.Error("три строки прозы не пойманы")
	}
	link := Comment{Lines: 1, Text: "См. https://example.com/" + strings.Repeat("a", 120), Next: "func f() {"}
	if hasRule(structChecks(2, 100, link), "Long comment line") {
		t.Error("строка со ссылкой оштрафована за длину")
	}
}

func TestKeepTagMutesBlock(t *testing.T) {
	cs := CodeSets{MaxLines: 2, MaxLineLen: 100}
	var err error
	if cs.Code, err = humanize.LoadBuiltin("code"); err != nil {
		t.Fatal(err)
	}
	noisy := Comment{Lines: 1, Text: "old := compute()\nvar x = 1", Lang: "go"}
	if len(CheckComment(cs, noisy, "code")) == 0 {
		t.Fatal("закомментированный код должен ловиться, иначе тест ни о чём")
	}
	kept := noisy
	kept.Text = "hum1izer:keep нужен под рукой\n" + noisy.Text
	if got := CheckComment(cs, kept, "code"); len(got) != 0 {
		t.Errorf("блок с hum1izer:keep всё равно даёт находки: %+v", got)
	}
}

// Правило глушится жанром поштучно: "linked to" в коде - связь в модели данных,
// в прозе - уход от конкретики, и там оно должно остаться.
func TestGenreMutesSingleRule(t *testing.T) {
	var cs CodeSets
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

	c := Comment{File: "a.ts", Start: 1, Lines: 1,
		Text: "The object a conversation is linked to, as the root message would carry it.",
		Next: "const x = 1"}
	if hasRule(CheckComment(cs, c, "code"), "linked to / tied to") {
		t.Error("заглушённое правило сработало в коде")
	}
	if !hasRule(CheckComment(cs, c, "marketing"), "linked to / tied to") {
		t.Error("правило пропало и в прозе, а глушился только код")
	}
	// Соседи по категории не должны пострадать: transformation из Tier 2
	// заглушен, unleash из той же категории - нет.
	if !hasRule(CheckComment(cs, Comment{File: "a.ts", Start: 1, Lines: 1,
		Text: "This will unleash the full power of the cache", Next: "const x = 1"}, "code"), "unleash") {
		t.Error("вместе с правилом заглушена вся категория")
	}
}

// C и C++ разбираются тем же сканером, что Swift, но без regexLit: деление в
// сишном коде сплошь и рядом, а литералов-регулярок там нет.
func TestExtractCommentsC(t *testing.T) {
	src := "/*\n * Returns free slots.\n */\nstatic int f(void)\n{\n\t/* head wraps before tail */\n\treturn 10 / 2; // деление, не регулярка\n}\n"
	got := texts(extract(t, "a.c", src))
	if len(got) != 3 {
		t.Fatalf("блоков %d, ожидалось 3: %q", len(got), got)
	}
	if !strings.Contains(got[0], "Returns free slots") {
		t.Errorf("первый блок: %q", got[0])
	}
	if !strings.Contains(got[2], "деление") {
		t.Errorf("хвостовой комментарий после деления потерян: %q", got)
	}
	if h := texts(extract(t, "a.h", "/* guard */\n#define A 1\n")); len(h) != 1 {
		t.Errorf(".h не разобрался: %q", h)
	}
}
