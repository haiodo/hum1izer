package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Хуки бывают двух видов: JSON-настройки в стиле Claude Code и плагин отдельным файлом. Чужие ключи
// и хуки в настройках должны уцелеть.
type hookSpec struct {
	event   string
	matcher string
	args    string
}

var hookSpecs = []hookSpec{
	{event: "SessionStart", matcher: "startup|resume|clear", args: "hook session-start"},
	// SessionStart в субагентах не срабатывает, правила им несёт SubagentStart.
	{event: "SubagentStart", matcher: "", args: "hook subagent-start"},
	// Матчер - регулярка по имени инструмента, а имена у агентов разные:
	// Claude Code правит файл через Edit и Write, Codex - через apply_patch.
	{event: "PostToolUse", matcher: "Edit|Write|apply_patch", args: "hook post-edit"},
}

// hookTarget - куда и как ставятся хуки для одного агента: JSON-настройки,
// файл-плагин из plugins/ или свод правил в AGENTS.md.
type hookTarget struct {
	flag   string
	file   string   // относительно домашнего каталога
	at     []string // ключи до карты событий внутри JSON
	plugin string   // имя файла в plugins/
	rules  bool
	toml   bool // [[hooks]] в config.toml: слияние маркерным блоком
}

var hookTargets = []hookTarget{
	{flag: "claude", file: ".claude/settings.json", at: []string{"hooks"}},
	{flag: "codex", file: ".codex/hooks.json", at: []string{"hooks"}},
	{flag: "kimi", file: ".kimi-code/config.toml", toml: true},
	{flag: "opencode", file: ".config/opencode/plugin/hum1izer.js", plugin: "opencode.js"},
	{flag: "pi", file: ".pi/agent/extensions/hum1izer.ts", plugin: "pi.ts"},
	// DSH читает $DSH_HOME/AGENTS.md и <agentsHome>/AGENTS.md на старте сессии,
	// так что здесь хватает текста; проверка после правки остаётся ручной.
	{flag: "dsh", file: ".dsh/AGENTS.md", rules: true},
	{flag: "agents", file: ".agents/AGENTS.md", rules: true},
}

func hookTargetFor(flag string) (hookTarget, bool) {
	for _, t := range hookTargets {
		if t.flag == flag {
			return t, true
		}
	}
	return hookTarget{}, false
}

// installHooks ставит хуки для выбранных агентов. Уже стоящие перезаписываются
// только с --force: у пользователя там может быть своя правка.
func installHooks(base, kimiBase string, kimiEnv bool, flags []string, force bool) int {
	exit := 0
	for _, flag := range flags {
		t, ok := hookTargetFor(flag)
		if !ok {
			fmt.Printf("  - %-10s no hooks for this agent\n", flag)
			continue
		}
		root := targetBase(base, kimiBase, kimiEnv, t.flag)
		rel := t.file
		if t.flag == "kimi" && kimiEnv {
			rel = strings.TrimPrefix(rel, ".kimi-code/")
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		var err error
		switch {
		case t.rules:
			err = writeRules(t, path)
		case t.plugin != "":
			err = writePlugin(t, path, force)
		case t.toml:
			err = writeHookToml(t, path, force)
		default:
			err = writeHookSettings(t, path, force)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s hooks: %v\n", flag, err)
			exit = 2
		}
	}
	return exit
}

const (
	rulesStart = "<!-- hum1izer:start -->"
	rulesEnd   = "<!-- hum1izer:end -->"
)

// rulesBlock - блок для глобального AGENTS.md. Маркеры дают повторной установке
// заменить свой блок и не тронуть чужие строки в том же файле.
func rulesBlock() string {
	return rulesStart + "\n## Comment rules (hum1izer)\n\n" + Rules() + "\n\n" +
		"After editing a file, check what changed: `hum1izer --code --format md <file>`.\n" +
		rulesEnd
}

func writeRules(t hookTarget, path string) error {
	block := rulesBlock()
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(path); err == nil {
			fmt.Printf("  - %-10s %s is a symlink to %s; the block goes to the target\n", t.flag, path, target)
		}
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	out := string(existing)
	if start := strings.Index(out, rulesStart); start >= 0 {
		end := len(out)
		if rel := strings.Index(out[start:], rulesEnd); rel >= 0 {
			end = start + rel + len(rulesEnd)
		}
		if out[start:end] == block {
			fmt.Printf("  = %-10s rules already installed: %s\n", t.flag, path)
			return nil
		}
		if err := writeRulesFile(path, out[:start]+block+out[end:]); err != nil {
			return err
		}
		fmt.Printf("  + %-10s rules updated: %s\n", t.flag, path)
		return nil
	}
	if out != "" {
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += "\n"
	}
	if err := writeRulesFile(path, out+block+"\n"); err != nil {
		return err
	}
	fmt.Printf("  + %-10s rules: %s\n", t.flag, path)
	return nil
}

func writeRulesFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func writePlugin(t hookTarget, path string, force bool) error {
	data, err := plugins.ReadFile("plugins/" + t.plugin)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Printf("  = %-10s hooks already installed: %s (--force overwrites)\n", t.flag, path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("  + %-10s hooks: %s\n", t.flag, path)
	return nil
}

func writeHookSettings(t hookTarget, path string, force bool) error {
	root := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	case !os.IsNotExist(err):
		return err
	}

	self, err := os.Executable()
	if err != nil || self == "" {
		self = "hum1izer"
	}

	events := dig(root, t.at)
	added := 0
	for _, s := range hookSpecs {
		groups := asSlice(events[s.event])
		kept := dropOurs(groups, s.args)
		if len(kept) < len(groups) && !force {
			continue
		}
		events[s.event] = append(kept, map[string]any{
			"matcher": s.matcher,
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": quote(self) + " " + s.args,
				"timeout": 30,
			}},
		})
		added++
	}
	if added == 0 {
		fmt.Printf("  = %-10s hooks already installed: %s (--force overwrites)\n", t.flag, path)
		return nil
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("  + %-10s hooks: %s\n", t.flag, path)
	return nil
}

// dig достаёт вложенную карту по ключам, создавая недостающие уровни.
func dig(root map[string]any, keys []string) map[string]any {
	m := root
	for _, k := range keys {
		next, _ := m[k].(map[string]any)
		if next == nil {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// dropOurs убирает группы с нашей командой: иначе повторная установка
// размножает один и тот же хук. Узнаётся по хвосту команды, а не по имени
// бинаря - путь к нему у каждого свой.
func dropOurs(groups []any, args string) []any {
	out := make([]any, 0, len(groups))
	for _, g := range groups {
		if strings.HasSuffix(commandOf(g), args) {
			continue
		}
		out = append(out, g)
	}
	return out
}

func commandOf(group any) string {
	m, _ := group.(map[string]any)
	var b strings.Builder
	for _, h := range asSlice(m["hooks"]) {
		hm, _ := h.(map[string]any)
		cmd, _ := hm["command"].(string)
		b.WriteString(cmd)
	}
	return b.String()
}

// quote: в команде хука путь идёт как есть, кавычки нужны только на пробелах.
func quote(path string) string {
	if strings.ContainsAny(path, " \t") {
		return `"` + path + `"`
	}
	return path
}

const (
	hooksMarkStart = "# hum1izer:hooks:start"
	hooksMarkEnd   = "# hum1izer:hooks:end"
)

// writeHookToml дописывает наши [[hooks]] в config.toml Kimi. Свой блок стоит
// между маркерами: повторная установка заменяет его целиком, чужой текст не трогается.
func writeHookToml(t hookTarget, path string, force bool) error {
	self, err := os.Executable()
	if err != nil || self == "" {
		self = "hum1izer"
	}
	var b strings.Builder
	b.WriteString(hooksMarkStart + "\n")
	for _, s := range hookSpecs {
		fmt.Fprintf(&b, "[[hooks]]\nevent = %s\n", tomlStr(s.event))
		if s.matcher != "" {
			fmt.Fprintf(&b, "matcher = %s\n", tomlStr(s.matcher))
		}
		fmt.Fprintf(&b, "command = %s\ntimeout = 30\n\n", tomlStr(quote(self)+" "+s.args+" --text"))
	}
	b.WriteString(hooksMarkEnd + "\n")
	block := b.String()

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	out := string(existing)
	start, end, err := findHookBlock(out)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if start >= 0 {
		if out[start:end] == block {
			fmt.Printf("  = %-10s hooks already installed: %s\n", t.flag, path)
			return nil
		}
		if !force {
			fmt.Printf("  = %-10s hooks already installed: %s (--force overwrites)\n", t.flag, path)
			return nil
		}
		out = out[:start] + block + out[end:]
	} else {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += "\n" + block
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return err
	}
	fmt.Printf("  + %-10s hooks: %s\n", t.flag, path)
	return nil
}

// findHookBlock ищет маркеры блока как строки-комментарии вне многострочных
// строк TOML. start = -1, если маркеров нет; непарный маркер - ошибка.
func findHookBlock(out string) (start, end int, err error) {
	start, end = -1, -1    // именованные возвраты: -1 - маркеров нет
	var multi, open string // открытый """/''' и открытый маркер
	openAt := -1
	off := 0
	for _, line := range strings.SplitAfter(out, "\n") {
		text := strings.TrimSpace(strings.TrimRight(line, "\n"))
		var mark string
		if multi == "" {
			switch text {
			case hooksMarkStart:
				mark = hooksMarkStart
			case hooksMarkEnd:
				mark = hooksMarkEnd
			}
		}
		switch {
		case mark != "" && open == "":
			open, openAt = mark, off
		case mark == hooksMarkEnd && open == hooksMarkStart:
			return openAt, off + len(line), nil
		case mark != "":
			return -1, -1, fmt.Errorf("unpaired hum1izer marker in config.toml; fix or remove it by hand")
		default:
			if loc := scanTomlLine(text, multi); loc != "" {
				multi = loc
			} else if multi != "" {
				multi = ""
			}
		}
		off += len(line)
	}
	if open != "" {
		return -1, -1, fmt.Errorf("unpaired hum1izer marker in config.toml; fix or remove it by hand")
	}
	return start, end, nil
}

// scanTomlLine проходит строку TOML и возвращает открытый разделитель
// многострочной строки, если строка его открывает и не закрывает.
func scanTomlLine(line, multi string) string {
	for i := 0; i < len(line); {
		if multi != "" {
			if strings.HasPrefix(line[i:], multi) {
				i += len(multi)
				multi = ""
				continue
			}
			if multi == `"""` && line[i] == '\\' {
				i += 2
				continue
			}
			i++
			continue
		}
		switch line[i] {
		case '#':
			return ""
		case '"':
			if strings.HasPrefix(line[i:], `"""`) {
				multi = `"""`
				i += 3
			} else {
				i = skipTomlStr(line, i, '"')
			}
		case '\'':
			if strings.HasPrefix(line[i:], `'''`) {
				multi = `'''`
				i += 3
			} else {
				i = skipTomlStr(line, i, '\'')
			}
		default:
			i++
		}
	}
	return multi
}

// skipTomlStr пропускает однострочную строку, начиная с кавычки at; в basic
// строке backslash экранирует следующий знак.
func skipTomlStr(line string, at int, q byte) int {
	for i := at + 1; i < len(line); i++ {
		if q == '"' && line[i] == '\\' {
			i++
			continue
		}
		if line[i] == q {
			return i + 1
		}
	}
	return len(line)
}

// tomlStr кодирует строку как basic string TOML: экранируются \ и ".
func tomlStr(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
