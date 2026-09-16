package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haiodo/hum1izer/internal/baseline"
	"github.com/haiodo/hum1izer/internal/code"
)

func TestRenderComment(t *testing.T) {
	got := renderComment("\t", "// старый текст", "одна короткая строка", 100)
	if len(got) != 1 || got[0] != "\t// одна короткая строка" {
		t.Errorf("строчный комментарий: %q", got)
	}

	long := renderComment("", "// x", strings.Repeat("слово ", 40), 40)
	if len(long) < 2 {
		t.Errorf("длинный текст не перенесён: %q", long)
	}
	for _, l := range long {
		if len([]rune(l)) > 40 {
			t.Errorf("строка длиннее предела: %q", l)
		}
	}

	block := renderComment("  ", "  /** old */", strings.Repeat("word ", 30), 40)
	if block[0] != "  /**" || block[len(block)-1] != "   */" {
		t.Errorf("блочный комментарий собран неверно: %q", block)
	}
	// 30 + пробел + 5 ровно упирается в предел: при неверной ширине " * " строка
	// вылезает за maxLine, и следующий прогон ругается на свою же правку.
	// Однострочный /* ... */ добавляет " */": влезать должен вместе с ним.
	oneLine := renderComment("  ", "  /* old */", strings.Repeat("z", 33), 40)
	for _, l := range oneLine {
		if len([]rune(l)) > 40 {
			t.Errorf("однострочный блок длиннее предела: %q (%d)", l, len([]rune(l)))
		}
	}
	tight := renderComment("  ", "  /** old */", strings.Repeat("x", 30)+" "+strings.Repeat("y", 5), 40)
	for _, l := range tight {
		if len([]rune(l)) > 40 {
			t.Errorf("строка блока длиннее предела: %q (%d)", l, len([]rune(l)))
		}
	}
}

func TestPlanDeletesWholeLinesAndKeepsCode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := "package demo\n\nfunc f() int {\n\tx := 1 // хвост\n\t// целая строка\n\treturn x\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmts) != 2 {
		t.Fatalf("ожидали два блока, получили %d", len(cmts))
	}

	for _, c := range cmts {
		e, err := planAt(t, c, true, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		switch strings.TrimSpace(c.Text) {
		case "хвост":
			if e.new != "" {
				t.Errorf("хвостовой комментарий срезан неверно: %q", e.new)
			}
			if got := src[e.so:e.eo]; got != " // хвост" {
				t.Errorf("вырезано %q, ожидалось \" // хвост\"", got)
			}
		case "целая строка":
			if e.new != "" {
				t.Errorf("строка целиком должна исчезнуть, осталось %q", e.new)
			}
		default:
			t.Errorf("неожиданный блок %q", c.Text)
		}
	}
}

func TestPlanRefusesToReplaceTrailing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("package demo\n\nvar x = 1 // хвост\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planAt(t, cmts[0], false, "новый текст", 100); err == nil {
		t.Error("замена хвостового комментария должна отказывать")
	}
}

func TestPlanKeepsIndent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.ts")
	src := "function f (): void {\n  if (x) {\n        // старый текст\n        run()\n  }\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := planAt(t, cmts[0], false, "новый текст", 100)
	if err != nil {
		t.Fatal(err)
	}
	if e.new != "        // новый текст" {
		t.Errorf("отступ потерян: %q", e.new)
	}
}

func TestBatchTakesHashAndSkipsUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := "package demo\n\n// old := compute()\nvar x = 1\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := baseline.Hash(cmts[0].Text)

	batch := filepath.Join(dir, "work.jsonl")
	body := `{"hash":"` + hash + `","delete":true}` + "\n" +
		`{"hash":"0000deadbeef"}` + "\n"
	if err := os.WriteFile(batch, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc := fixBatch(dir, batch, "", true, 100); rc != 0 {
		t.Fatalf("пачка вернула %d, ожидался 0", rc)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "old := compute") {
		t.Errorf("блок по полю hash не удалён:\n%s", got)
	}
}

func TestPlanKeepsCodeAroundInlineComment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := "package demo\n\nfunc f(a, y int) int {\n\treturn g(a /* почему так */, y)\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := planAt(t, cmts[0], true, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := src[:e.so] + e.new + src[e.eo:]
	if !strings.Contains(got, ", y)") {
		t.Errorf("код после комментария потерян:\n%s", got)
	}
}

func TestPlanIgnoresSameTextInStringLiteral(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := "package demo\n\nconst marker = \"// сборка упала\" // сборка упала\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := planAt(t, cmts[0], true, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := src[:e.so] + e.new + src[e.eo:]
	if !strings.Contains(got, "const marker = \"// сборка упала\"") {
		t.Errorf("вырезан литерал вместо комментария:\n%s", got)
	}
}

func TestPlanKeepsCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := "package demo\r\n\r\n// старый текст\r\nvar x = 1\r\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := planAt(t, cmts[0], false, strings.Repeat("слово ", 30), 40)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.new, "\r\n") {
		t.Errorf("перенос без CR в CRLF-файле: %q", e.new)
	}
	got := src[:e.so] + e.new + src[e.eo:]
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("в файле появились смешанные концы строк:\n%q", got)
	}
}

// planAt - plan для теста: файл читает сам, чтобы в каждом тесте не повторять.
func planAt(t *testing.T, c code.Comment, del bool, body string, maxLine int) (edit, error) {
	t.Helper()
	raw, err := os.ReadFile(c.File)
	if err != nil {
		t.Fatal(err)
	}
	return plan(c, string(raw), del, body, maxLine)
}

func TestPlanKeepsCRLFWhenDeletingTrailingComment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := "package demo\r\n\r\nvar x = 1 // хвост\r\nvar y = 2\r\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := planAt(t, cmts[0], true, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := src[:e.so] + e.new + src[e.eo:]
	if got != "package demo\r\n\r\nvar x = 1\r\nvar y = 2\r\n" {
		t.Errorf("концы строк испорчены: %q", got)
	}
}

func TestPlaceholderRejectsTemplateText(t *testing.T) {
	for _, s := range []string{"...", " ... ", "", "<new text>"} {
		if !placeholder(s) {
			t.Errorf("шаблон %q принят за текст", s)
		}
	}
	if placeholder("почему тут ждём ответа") {
		t.Error("настоящий текст принят за шаблон")
	}
}

func TestWalkPathsRefusesBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := walkPaths(dir); err != nil {
		t.Fatalf("без настроек обход должен работать: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hum1izer.yaml"), []byte("comments: [нет\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := walkPaths(dir); err == nil {
		t.Error("битые настройки должны останавливать правку, а не молча снимать исключения")
	}
}

func TestFixBlockEditsEveryCopy(t *testing.T) {
	dir := t.TempDir()
	src := "package demo\n\n// повторный комментарий\nvar x = 1\n"
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmts, err := code.ExtractComments(filepath.Join(dir, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := locateAll(dir, baseline.Hash(cmts[0].Text))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("нашли %d копий, ожидали 2", len(blocks))
	}
}

func TestBatchFileNarrowsEditToOneCopy(t *testing.T) {
	dir := t.TempDir()
	src := "package demo\n\n// No transformation, just pass through\nvar x = 1\n"
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmts, err := code.ExtractComments(filepath.Join(dir, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	hash := baseline.Hash(cmts[0].Text)

	batch := filepath.Join(dir, "work.jsonl")
	line := `{"hash":"` + hash + `","file":"` + filepath.Join(dir, "a.go") + `","delete":true}` + "\n"
	if err := os.WriteFile(batch, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc := fixBatch(dir, batch, "", true, 100); rc != 0 {
		t.Fatalf("пачка вернула %d, ожидался 0", rc)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	b, _ := os.ReadFile(filepath.Join(dir, "b.go"))
	if strings.Contains(string(a), "No transformation") {
		t.Error("в указанном файле блок не удалён")
	}
	if !strings.Contains(string(b), "No transformation") {
		t.Error("правка ушла в файл, который не просили")
	}
}

func TestBatchWithoutFileEditsEveryCopy(t *testing.T) {
	dir := t.TempDir()
	src := "package demo\n\n// No transformation, just pass through\nvar x = 1\n"
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmts, err := code.ExtractComments(filepath.Join(dir, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	batch := filepath.Join(dir, "work.jsonl")
	line := `{"hash":"` + baseline.Hash(cmts[0].Text) + `","delete":true}` + "\n"
	if err := os.WriteFile(batch, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc := fixBatch(dir, batch, "", true, 100); rc != 0 {
		t.Fatalf("пачка вернула %d, ожидался 0", rc)
	}
	for _, name := range []string{"a.go", "b.go"} {
		got, _ := os.ReadFile(filepath.Join(dir, name))
		if strings.Contains(string(got), "No transformation") {
			t.Errorf("%s: копия блока осталась", name)
		}
	}
}

// Сканер не для Go забирает в блок и \r - при замене его нельзя срезать вместе
// с текстом, иначе в файле появляются смешанные концы строк.
func TestPlanKeepsCRLFInNonGoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.ts")
	src := "const x = 1;\r\n\r\n// старый текст\r\nconst y = 2;\r\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := planAt(t, cmts[0], false, strings.Repeat("слово ", 30), 40)
	if err != nil {
		t.Fatal(err)
	}
	got := src[:e.so] + e.new + src[e.eo:]
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("смешанные концы строк:\n%q", got)
	}
}

func TestPlanDeletesInlineCommentInCRLFFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.ts")
	src := "const x = f(a /* почему */, y);\r\nconst z = 3;\r\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := planAt(t, cmts[0], true, "", 40)
	if err != nil {
		t.Fatal(err)
	}
	got := src[:e.so] + e.new + src[e.eo:]
	if !strings.Contains(got, "f(a , y)") {
		t.Errorf("код вокруг комментария потерян: %q", got)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("смешанные концы строк:\n%q", got)
	}
}

func TestRenderCommentKeepsMarkerKind(t *testing.T) {
	short := renderComment("  ", "  <!-- старый -->", "короткий текст", 100)
	if len(short) != 1 || short[0] != "  <!-- короткий текст -->" {
		t.Errorf("html-комментарий в одну строку: %q", short)
	}

	long := renderComment("", "<!-- старый -->", strings.Repeat("слово ", 30), 40)
	if long[0] != "<!--" || long[len(long)-1] != "-->" {
		t.Errorf("html-комментарий блоком: %q", long)
	}
	for _, l := range long {
		if strings.HasPrefix(strings.TrimSpace(l), "//") {
			t.Errorf("разметка получила // вместо <!-- -->: %q", long)
		}
	}

	// Обычный /* не должен превращаться в /**: это разные вещи для IDE и доков.
	plain := renderComment("", "/* старый */", strings.Repeat("слово ", 30), 40)
	if plain[0] != "/*" {
		t.Errorf("обычный блок стал %q", plain[0])
	}
	doc := renderComment("", "/** старый */", strings.Repeat("слово ", 30), 40)
	if doc[0] != "/**" {
		t.Errorf("doc-блок потерял вторую звезду: %q", doc[0])
	}
	docShort := renderComment("", "/** старый */", "короткий текст", 100)
	if len(docShort) != 1 || docShort[0] != "/** короткий текст */" {
		t.Errorf("однострочный doc-блок: %q", docShort)
	}
}

func TestKeepPutsBlockIntoBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := "package demo\n\n// old := compute()\nvar x = 1\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmts, err := code.ExtractComments(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := baseline.Hash(cmts[0].Text)
	base := filepath.Join(dir, "snapshot")

	batch := filepath.Join(dir, "work.jsonl")
	if err := os.WriteFile(batch, []byte(`{"hash":"`+hash+`","keep":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc := fixBatch(dir, batch, base, true, 100); rc != 0 {
		t.Fatalf("пачка вернула %d, ожидался 0", rc)
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "old := compute()") {
		t.Error("keep не должен править файл")
	}
	set, err := baseline.Load(base)
	if err != nil {
		t.Fatal(err)
	}
	if !set.Known(baseline.Entry{Hash: hash, Rule: "Commented-out code"}) {
		t.Errorf("блок не попал в снимок: %s", base)
	}
}
