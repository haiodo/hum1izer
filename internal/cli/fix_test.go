package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haiodo/hum1izer/internal/code"
)

func TestRenderComment(t *testing.T) {
	got := renderComment("\t// старый текст", "одна короткая строка", 100)
	if len(got) != 1 || got[0] != "\t// одна короткая строка" {
		t.Errorf("строчный комментарий: %q", got)
	}

	long := renderComment("// x", strings.Repeat("слово ", 40), 40)
	if len(long) < 2 {
		t.Errorf("длинный текст не перенесён: %q", long)
	}
	for _, l := range long {
		if len([]rune(l)) > 40 {
			t.Errorf("строка длиннее предела: %q", l)
		}
	}

	block := renderComment("  /** old */", strings.Repeat("word ", 30), 40)
	if block[0] != "  /**" || block[len(block)-1] != "   */" {
		t.Errorf("блочный комментарий собран неверно: %q", block)
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
		e, err := plan(c, true, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		switch strings.TrimSpace(c.Text) {
		case "хвост":
			if len(e.new) != 1 || e.new[0] != "\tx := 1" {
				t.Errorf("хвостовой комментарий срезан неверно: %q", e.new)
			}
		case "целая строка":
			if len(e.new) != 0 {
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
	if _, err := plan(cmts[0], false, "новый текст", 100); err == nil {
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
	e, err := plan(cmts[0], false, "новый текст", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.new) != 1 || e.new[0] != "        // новый текст" {
		t.Errorf("отступ потерян: %q", e.new)
	}
}
