package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

// DeepSeek Harness сканирует ~/.dsh/skills первым из пользовательских корней,
// а из шапки читает name, description и user-invocable.
func TestInstallDsh(t *testing.T) {
	dir := t.TempDir()
	if code := Run([]string{"--dsh", "--dir", dir}, "test"); code != 0 {
		t.Fatalf("install вернул %d", code)
	}
	path := filepath.Join(dir, ".dsh", "skills", "hum1izer", "SKILL.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("скилл для DSH не установлен: %v", err)
	}
	parts := strings.SplitN(string(b), "---", 3)
	if len(parts) < 3 {
		t.Fatal("нет шапки")
	}
	var head map[string]any
	if err := yaml.Unmarshal([]byte(parts[1]), &head); err != nil {
		t.Fatalf("шапка не разбирается как YAML: %v", err)
	}
	if head["name"] != "hum1izer" {
		t.Errorf("name = %v, ожидалось hum1izer", head["name"])
	}
	if d, _ := head["description"].(string); d == "" {
		t.Error("DSH требует непустое description")
	}
	if head["user-invocable"] != true {
		t.Errorf("user-invocable = %v, ожидалось true", head["user-invocable"])
	}
	if !strings.Contains(string(b), "Fact lock") {
		t.Error("тело скилла не попало в файл")
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
	if !strings.Contains(string(b), "hook subagent-start") {
		t.Error("хук subagent-start не прописан")
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
		if rel == ".config/opencode/plugin/hum1izer.js" {
			for _, want := range []string{"export default", `id: "hum1izer"`, `ctx.session.hook("context"`, `ctx.tool.hook("execute.after"`, "event.result.output"} {
				if !strings.Contains(string(b), want) {
					t.Errorf("%s: нет API OpenCode 2 %q", rel, want)
				}
			}
		}
	}
}

// Kimi держит хуки в [[hooks]] user-level config.toml: установка дописывает
// свой блок, чужие ключи и чужие хуки уцелевают, повторный прогон не дублирует.
func TestInstallKimiHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".kimi-code", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const mine = "model = \"k2\"\ntheme = \"dark\"\n\n" +
		"[[hooks]]\nevent = \"Notification\"\nmatcher = \"task.completed\"\ncommand = \"notify-send done\"\n"
	if err := os.WriteFile(path, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if code := Run([]string{"--kimi", "--hooks", "--dir", dir}, "test"); code != 0 {
			t.Fatalf("install вернул %d", code)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`model = "k2"`, "notify-send done"} {
		if !strings.Contains(s, want) {
			t.Errorf("чужая настройка не уцелела: нет %q", want)
		}
	}
	for _, want := range []string{`event = "SessionStart"`, `event = "SubagentStart"`, `event = "PostToolUse"`,
		`matcher = "Edit|Write|apply_patch"`, "hook post-edit --text"} {
		if !strings.Contains(s, want) {
			t.Errorf("в config.toml нет %q", want)
		}
	}
	if n := strings.Count(s, "hum1izer:hooks:start"); n != 1 {
		t.Errorf("блок hum1izer встречается %d раз, ожидался один", n)
	}
}

// Маркер внутри многострочной строки TOML - не маркер: установка дописывает
// свой блок после неё, а не режет до EOF. Непарный реальный маркер - ошибка
// без изменения файла, даже с --force.
func TestInstallKimiHooksMultilineString(t *testing.T) {
	cases := []struct{ name, text string }{
		{"basic", "text = \"\"\"multi\n# hum1izer:hooks:start\nline\"\"\"\n\n[tail]\nkey = 1\n"},
		{"literal", "text = '''multi\n# hum1izer:hooks:start\nline'''\n\n[tail]\nkey = 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".kimi-code", "config.toml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.text), 0o600); err != nil {
				t.Fatal(err)
			}
			if code := Run([]string{"--kimi", "--hooks", "--dir", dir}, "test"); code != 0 {
				t.Fatalf("install вернул %d", code)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			s := string(b)
			for _, want := range []string{"[tail]", "key = 1", `event = "SessionStart"`} {
				if !strings.Contains(s, want) {
					t.Errorf("нет %q после установки:\n%s", want, s)
				}
			}
		})
	}
}

func TestInstallKimiHooksUnpairedMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".kimi-code", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const mine = "# hum1izer:hooks:start\n\n[tail]\nkey = 1\n"
	if err := os.WriteFile(path, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--kimi", "--hooks", "--dir", dir},
		{"--kimi", "--hooks", "--force", "--dir", dir}} {
		if code := Run(args, "test"); code == 0 {
			t.Fatalf("install %v вернул 0 на непарном маркере", args)
		}
		if b, _ := os.ReadFile(path); string(b) != mine {
			t.Fatalf("файл изменён при %v:\n%s", args, b)
		}
	}
}

// в пути; --dir перекрывает переменную окружения.
// С заданным KIMI_CODE_HOME скилл и config.toml ставятся в него, без .kimi-code
// в пути; --dir перекрывает переменную окружения.
func TestInstallKimiEnvHome(t *testing.T) {
	env := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", env)
	if code := Run([]string{"--kimi", "--hooks"}, "test"); code != 0 {
		t.Fatalf("install вернул %d", code)
	}
	for _, rel := range []string{"skills/hum1izer/SKILL.md", "config.toml"} {
		if _, err := os.Stat(filepath.Join(env, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s не установлен в KIMI_CODE_HOME: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(env, ".kimi-code")); !os.IsNotExist(err) {
		t.Error("лишний префикс .kimi-code в KIMI_CODE_HOME")
	}
	dir := t.TempDir()
	if code := Run([]string{"--kimi", "--dir", dir}, "test"); code != 0 {
		t.Fatalf("install --dir вернул %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".kimi-code", "skills", "hum1izer", "SKILL.md")); err != nil {
		t.Errorf("--dir не перекрыл KIMI_CODE_HOME: %v", err)
	}
}

// Kimi читает скилл из $KIMI_CODE_HOME/skills/<name>/SKILL.md; name и
// description обязательны.
func TestInstallKimiSkill(t *testing.T) {
	dir := t.TempDir()
	if code := Run([]string{"--kimi", "--dir", dir}, "test"); code != 0 {
		t.Fatalf("install вернул %d", code)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".kimi-code", "skills", "hum1izer", "SKILL.md"))
	if err != nil {
		t.Fatalf("скилл для Kimi не установлен: %v", err)
	}
	parts := strings.SplitN(string(b), "---", 3)
	if len(parts) < 3 {
		t.Fatal("нет шапки")
	}
	var head map[string]any
	if err := yaml.Unmarshal([]byte(parts[1]), &head); err != nil {
		t.Fatalf("шапка не разбирается как YAML: %v", err)
	}
	if head["name"] != "hum1izer" {
		t.Errorf("name = %v, ожидалось hum1izer", head["name"])
	}
	if d, _ := head["description"].(string); d == "" {
		t.Error("Kimi требует непустое description")
	}
}

// .agents - это блок правил в файле, а не JSON-настройки.
// DSH читает глобальный AGENTS.md в начале сессии: хуки для него и для общего
// .agents - это блок правил в файле, а не JSON-настройки.
func TestInstallHooksRules(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, ".agents", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(agents), 0o755); err != nil {
		t.Fatal(err)
	}
	const mine = "# мой файл\n\nсвои правила\n"
	if err := os.WriteFile(agents, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if code := Run([]string{"--agents", "--dsh", "--hooks", "--dir", dir}, "test"); code != 0 {
			t.Fatalf("install вернул %d", code)
		}
	}
	b, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.HasPrefix(s, mine) {
		t.Error("чужой текст в AGENTS.md не уцелел")
	}
	if n := strings.Count(s, "<!-- hum1izer:start -->"); n != 1 {
		t.Errorf("блок правил встречается %d раз, ожидался один", n)
	}
	if !strings.Contains(s, "Fact lock") {
		t.Error("свод правил не попал в AGENTS.md")
	}
	if _, err := os.Stat(filepath.Join(dir, ".dsh", "AGENTS.md")); err != nil {
		t.Errorf("для DSH нет .dsh/AGENTS.md: %v", err)
	}
}

// AGENTS.md часто бывает ссылкой на общий файл правил: блок должен уйти в цель,
// а ссылка - остаться ссылкой.
func TestInstallHooksRulesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "rules.md")
	if err := os.WriteFile(target, []byte("общие правила\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".dsh", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"--dsh", "--hooks", "--dir", dir}, "test"); code != 0 {
		t.Fatalf("install вернул %d", code)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Error("ссылка заменена обычным файлом")
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "общие правила") || !strings.Contains(string(b), "Fact lock") {
		t.Error("блок правил не попал в цель ссылки")
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
