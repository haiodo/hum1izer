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
	// Матчер - регулярка по имени инструмента, а имена у агентов разные:
	// Claude Code и ZCode правят файл через Edit и Write, Codex - через apply_patch.
	{event: "PostToolUse", matcher: "Edit|Write|apply_patch", args: "hook post-edit"},
}

// hookTarget - куда и как ставятся хуки для одного агента. Заполнено либо
// полями JSON-настроек, либо plugin - именем файла в plugins/.
type hookTarget struct {
	flag   string
	file   string   // относительно домашнего каталога
	at     []string // ключи до карты событий внутри JSON
	enable []string // ключ, которым агент включает хуки, если требует этого явно
	plugin string   // имя файла в plugins/
}

var hookTargets = []hookTarget{
	{flag: "claude", file: ".claude/settings.json", at: []string{"hooks"}},
	{flag: "zcode", file: ".zcode/cli/config.json", at: []string{"hooks", "events"},
		enable: []string{"hooks", "enabled"}},
	{flag: "codex", file: ".codex/hooks.json", at: []string{"hooks"}},
	{flag: "opencode", file: ".config/opencode/plugin/hum1izer.js", plugin: "opencode.js"},
	{flag: "pi", file: ".pi/agent/extensions/hum1izer.ts", plugin: "pi.ts"},
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
func installHooks(base string, flags []string, force bool) int {
	exit := 0
	for _, flag := range flags {
		t, ok := hookTargetFor(flag)
		if !ok {
			fmt.Printf("  - %-10s no hooks for this agent\n", flag)
			continue
		}
		path := filepath.Join(base, filepath.FromSlash(t.file))
		var err error
		if t.plugin != "" {
			err = writePlugin(t, path, force)
		} else {
			err = writeHookSettings(t, path, force)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s hooks: %v\n", flag, err)
			exit = 2
		}
	}
	return exit
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
	if len(t.enable) > 0 {
		last := len(t.enable) - 1
		dig(root, t.enable[:last])[t.enable[last]] = true
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
