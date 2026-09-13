package skill

import (
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
		if !strings.Contains(s, "--format jsonl") {
			t.Errorf("%s: в теле нет цикла правки", tg.Flag)
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
