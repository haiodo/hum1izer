package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCfg(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, Name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindWalksUp(t *testing.T) {
	root := t.TempDir()
	writeCfg(t, root, "version: 1\nrules:\n  disable: [Длинное тире]\n")
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := Find(deep)
	if err != nil {
		t.Fatal(err)
	}
	if c.Path == "" {
		t.Fatal("настройки не найдены выше по дереву")
	}
	if c.Allow("Длинное тире", "") {
		t.Error("отключённое правило прошло")
	}
	if !c.Allow("Пересказ кода", "") {
		t.Error("не отключённое правило отсеяно")
	}
}

func TestFindMissingIsNotError(t *testing.T) {
	c, err := Find(t.TempDir())
	if err != nil {
		t.Fatalf("отсутствие файла стало ошибкой: %v", err)
	}
	if c.Path != "" {
		t.Errorf("нашёлся неожиданный файл: %s", c.Path)
	}
	if !c.Allow("что угодно", "") {
		t.Error("пустые настройки не должны ничего отключать")
	}
}

func TestUnknownKeyFails(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, "version: 1\ncoments:\n  max_lines: 2\n")
	if _, err := Find(dir); err == nil {
		t.Error("опечатка в ключе принята молча")
	}
}

func TestUnknownLangFails(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, "version: 1\nlanguages:\n  ignore: [brainfuck]\n")
	if _, err := Find(dir); err == nil {
		t.Error("неизвестный язык принят молча")
	}
}

func TestAllowedLangs(t *testing.T) {
	var c Config
	if c.AllowedLangs() != nil {
		t.Error("пустые настройки должны разрешать все языки")
	}
	c.Languages.Ignore = []string{"swift"}
	got := c.AllowedLangs()
	if got["swift"] || !got["go"] {
		t.Errorf("ignore не сработал: %v", got)
	}
	c = Config{}
	c.Languages.Only = []string{"go"}
	got = c.AllowedLangs()
	if !got["go"] || got["ts"] {
		t.Errorf("only не сработал: %v", got)
	}
}

func TestOnlyIsWhitelist(t *testing.T) {
	var c Config
	c.Rules.Only = []string{"Пересказ кода"}
	if !c.Allow("Пересказ кода", "") {
		t.Error("правило из only отсеяно")
	}
	if c.Allow("Длинный комментарий", "Структура комментария") {
		t.Error("правило вне only прошло")
	}
	// Категория в only пускает все правила этой категории.
	c.Rules.Only = []string{"Структура комментария"}
	if !c.Allow("Длинный комментарий", "Структура комментария") {
		t.Error("категория в only не пропустила своё правило")
	}
}

func TestGlob(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"**/testdata/**", "a/b/testdata/x.go", true},
		{"**/testdata/**", "a/testdata.go", false},
		{"docs/*.go", "docs/a.go", true},
		{"docs/*.go", "docs/sub/a.go", false},
		{"*.pb.go", "a.pb.go", true},
		{"internal/**", "internal/code/check.go", true},
	}
	for _, c := range cases {
		re, err := Compile(c.glob)
		if err != nil {
			t.Fatalf("%s: %v", c.glob, err)
		}
		if got := re.MatchString(c.path); got != c.want {
			t.Errorf("%s против %s = %v, ожидалось %v", c.glob, c.path, got, c.want)
		}
	}
}

func TestLangOf(t *testing.T) {
	for path, want := range map[string]string{
		"a.go": "go", "a.tsx": "ts", "a.mjs": "js",
		"a.svelte": "svelte", "a.swift": "swift", "a.py": "",
	} {
		if got := LangOf(path); got != want {
			t.Errorf("%s = %q, ожидалось %q", path, got, want)
		}
	}
}
