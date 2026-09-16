package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAll(t *testing.T) {
	dir := t.TempDir()
	if code := Run([]string{"--all", "--dir", dir}, "test"); code != 0 {
		t.Fatalf("install вернул %d", code)
	}
	for _, tg := range targets {
		path := filepath.Join(dir, filepath.FromSlash(tg.Dir), "SKILL.md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", tg.Flag, err)
		}
		s := string(b)
		if !strings.HasPrefix(s, "---\nname: hum1izer\n") {
			t.Errorf("%s: нет шапки Agent Skills", tg.Flag)
		}
		if !strings.Contains(s, "Fact lock") || strings.Contains(s, "<!-- rules -->") {
			t.Errorf("%s: правила не подставлены в тело", tg.Flag)
		}
		ref := filepath.Join(filepath.Dir(path), "reference", "fixing.md")
		if b, err := os.ReadFile(ref); err != nil || !strings.Contains(string(b), "--format jsonl") {
			t.Errorf("%s: рядом нет reference/fixing.md с циклом правки", tg.Flag)
		}
	}
}

func TestInstallKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(targets[0].Dir), "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("моё"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"--claude", "--dir", dir}, "test"); code != 0 {
		t.Fatalf("install вернул %d", code)
	}
	if b, _ := os.ReadFile(path); string(b) != "моё" {
		t.Error("существующий скилл перезаписан без --force")
	}
	if code := Run([]string{"--claude", "--dir", dir, "--force"}, "test"); code != 0 {
		t.Fatalf("install --force вернул %d", code)
	}
	if b, _ := os.ReadFile(path); string(b) == "моё" {
		t.Error("--force не перезаписал")
	}
}

func TestNoTargetsIsError(t *testing.T) {
	if code := Run([]string{"--dir", t.TempDir()}, "test"); code != 2 {
		t.Errorf("без агентов ожидался код 2, получен %d", code)
	}
}

// SKILL.md в корне репозитория читают каталоги скиллов. Он генерируется из
// того же body.md, поэтому не должен расходиться с ним молча.
func TestRepoSkillUpToDate(t *testing.T) {
	got, err := os.ReadFile(filepath.Join("..", "..", "SKILL.md"))
	if err != nil {
		t.Fatalf("SKILL.md в корне нет: %v (собрать: make skill)", err)
	}
	if string(got) != RepoFile() {
		t.Error("SKILL.md разошёлся с body.md, пересобери: make skill")
	}
	for _, want := range []string{"## Install", "## Supported assistants", "homepage:"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("в SKILL.md нет раздела %q", want)
		}
	}
}

func TestInstallHooksTwice(t *testing.T) {
	dir := t.TempDir()
	for range 2 {
		if code := Run([]string{"--claude", "--hooks", "--dir", dir}, "test"); code != 0 {
			t.Fatalf("install вернул %d", code)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "hook post-edit"); n != 1 {
		t.Errorf("хук post-edit прописан %d раз, ожидался один", n)
	}
	if !strings.Contains(string(b), "hook session-start") {
		t.Error("хук session-start не прописан")
	}
}

func TestInstallHooksPlugins(t *testing.T) {
	dir := t.TempDir()
	if code := Run([]string{"--opencode", "--pi", "--hooks", "--dir", dir}, "test"); code != 0 {
		t.Fatalf("install вернул %d", code)
	}
	for _, rel := range []string{".config/opencode/plugin/hum1izer.js", ".pi/agent/extensions/hum1izer.ts"} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if !strings.Contains(string(b), "post-edit") {
			t.Errorf("%s: плагин не зовёт post-edit", rel)
		}
	}
}

func TestInstallHooksZcode(t *testing.T) {
	dir := t.TempDir()
	if code := Run([]string{"--zcode", "--hooks", "--dir", dir}, "test"); code != 0 {
		t.Fatalf("install вернул %d", code)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".zcode", "cli", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks struct {
			Enabled bool                     `json:"enabled"`
			Events  map[string][]interface{} `json:"events"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Hooks.Enabled {
		t.Error("hooks.enabled не выставлен, zcode такие хуки не запустит")
	}
	for _, e := range []string{"SessionStart", "PostToolUse"} {
		if len(cfg.Hooks.Events[e]) != 1 {
			t.Errorf("hooks.events.%s: %d групп, ожидалась одна", e, len(cfg.Hooks.Events[e]))
		}
	}
}

func TestInstallHooksKeepsForeign(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const mine = `{"model":"opus","hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"my-linter"}]}]}}`
	if err := os.WriteFile(path, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"--claude", "--hooks", "--dir", dir}, "test"); code != 0 {
		t.Fatalf("install вернул %d", code)
	}
	b, _ := os.ReadFile(path)
	for _, want := range []string{`"model": "opus"`, "my-linter", "hook post-edit"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("в settings.json нет %q", want)
		}
	}
}
