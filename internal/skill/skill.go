// Package skill раскладывает SKILL.md по каталогам агентов. Формат общий -
// Agent Skills (agentskills.io), различается только шапка и путь установки.
package skill

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed body.md
var body string

const name = "hum1izer"

// Описание двуязычное: по нему агент решает, брать скилл или нет, а просьба
// приходит то на английском, то на русском.
const description = "Check prose, code comments and commit messages for officialese, stock " +
	"phrases and traces of AI generation. Use when asked to make comments sound human, " +
	"clean up AI slop, or review text or commit messages. Русские триггеры: сделать " +
	"комментарии человечнее, почистить AI-слоп, отревьюить текст или коммиты."

// Target - куда и с какой шапкой класть скилл.
type Target struct {
	Flag        string
	Agent       string
	Dir         string // относительно домашнего каталога
	frontMatter string
}

var targets = []Target{
	{Flag: "claude", Agent: "Claude Code", Dir: ".claude/skills/" + name, frontMatter: `---
name: ` + name + `
description: "` + description + `"
user-invocable: true
license: MIT
---`},
	{Flag: "codex", Agent: "Codex", Dir: ".codex/skills/" + name, frontMatter: `---
name: ` + name + `
description: "` + description + `"
license: MIT
---`},
	{Flag: "hermes", Agent: "Hermes", Dir: ".hermes/skills/devops/" + name, frontMatter: `---
name: ` + name + `
description: "` + description + `"
version: 1.0.0
author: Andrey Sobolev
license: MIT
platforms: [linux, macos, windows]
metadata:
  hermes:
    tags: [code-quality, writing, comments, review]
---`},
	{Flag: "opencode", Agent: "opencode", Dir: ".config/opencode/skill/" + name, frontMatter: `---
name: ` + name + `
description: "` + description + `"
license: MIT
---`},
	{Flag: "pi", Agent: "pi", Dir: ".pi/agent/skills/" + name, frontMatter: `---
name: ` + name + `
description: "` + description + `"
license: MIT
---`},
	{Flag: "agents", Agent: "любой агент по стандарту Agent Skills", Dir: ".agents/skills/" + name, frontMatter: `---
name: ` + name + `
description: "` + description + `"
license: MIT
---`},
}

const usage = `hum1izer install - поставить скилл для агента.

  hum1izer install --claude --codex     выбранные агенты
  hum1izer install --all                все сразу
  hum1izer install --claude --dir .     в проект, а не в домашний каталог
  hum1izer install --print              напечатать SKILL.md и выйти

Агенты:
  --claude   ~/.claude/skills/hum1izer/SKILL.md
  --codex    ~/.codex/skills/hum1izer/SKILL.md
  --opencode ~/.config/opencode/skill/hum1izer/SKILL.md
  --hermes   ~/.hermes/skills/devops/hum1izer/SKILL.md
  --pi       ~/.pi/agent/skills/hum1izer/SKILL.md
  --agents   ~/.agents/skills/hum1izer/SKILL.md
             opencode читает и этот каталог, и ~/.claude/skills

Прочее:
  --all      все агенты сразу
  --dir D    корень вместо домашнего каталога
  --force    перезаписать, если файл уже есть
  --print    вывести SKILL.md в stdout
`

// Run выполняет подкоманду install. Возвращает код выхода.
func Run(args []string, version string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	picked := map[string]*bool{}
	for _, t := range targets {
		picked[t.Flag] = fs.Bool(t.Flag, false, "поставить для "+t.Agent)
	}
	all := fs.Bool("all", false, "все агенты сразу")
	root := fs.String("dir", "", "корень вместо домашнего каталога")
	force := fs.Bool("force", false, "перезаписать существующий файл")
	print := fs.Bool("print", false, "вывести SKILL.md в stdout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *print {
		fmt.Print(render(targets[0], version))
		return 0
	}

	var chosen []Target
	for _, t := range targets {
		if *all || *picked[t.Flag] {
			chosen = append(chosen, t)
		}
	}
	if len(chosen) == 0 {
		fs.Usage()
		return 2
	}

	base := *root
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "домашний каталог: %v\n", err)
			return 2
		}
		base = home
	}

	exit := 0
	for _, t := range chosen {
		path := filepath.Join(base, filepath.FromSlash(t.Dir), "SKILL.md")
		if _, err := os.Stat(path); err == nil && !*force {
			fmt.Printf("  = %-10s уже стоит: %s (--force перезапишет)\n", t.Flag, path)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", t.Flag, err)
			exit = 2
			continue
		}
		if err := os.WriteFile(path, []byte(render(t, version)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", t.Flag, err)
			exit = 2
			continue
		}
		fmt.Printf("  + %-10s %s\n", t.Flag, path)
	}
	return exit
}

func render(t Target, version string) string {
	return t.frontMatter + "\n\n" + strings.TrimSpace(body) +
		"\n\n<!-- поставлено hum1izer " + version + " -->\n"
}
