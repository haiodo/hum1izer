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

// Описание двуязычное: просьба приходит то на английском, то на русском.
// Предел листинга - 1536 знаков, места хватает, но повторы тут лишние.
const description = "Finds officialese, cliches and AI traces in prose, code comments and " +
	"commit messages. Ask it to humanize comments, clean AI slop, review text or commits. " +
	"Русский: очеловечить текст и комментарии, почистить AI-слоп, отревьюить коммиты."

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
  --repo     вывести SKILL.md для корня репозитория (make skill)
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
	repo := fs.Bool("repo", false, "вывести SKILL.md для корня репозитория")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repo {
		fmt.Print(RepoFile())
		return 0
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

// repoHead - шапка для SKILL.md в корне: каталоги скиллов читают этот файл и
// ждут в нём установку и список агентов рядом с телом инструкции.
const repoHead = `---
name: ` + name + `
description: "` + description + `"
license: MIT
homepage: https://github.com/haiodo/hum1izer
user-invocable: true
---

## Install

` + "```bash" + `
go install github.com/haiodo/hum1izer@latest   # или бинарь из releases
hum1izer install --all                         # разложить этот скилл по агентам
` + "```" + `

Готовые бинари под macOS, Linux и Windows - в
[releases](https://github.com/haiodo/hum1izer/releases). Сервисов и ключей
инструменту не нужно: всё считается локально.

## Supported assistants

Claude Code, Codex, opencode, Hermes, Pi, agents. ` + "`hum1izer install --all`" + `
кладёт SKILL.md в каталог каждого, ` + "`--claude`" + ` и остальные флаги - поштучно.
`

// RepoFile - содержимое SKILL.md для корня. Версия не подставляется: файл
// лежит в git, и строка с ней меняла бы его на каждом релизе впустую.
func RepoFile() string {
	return repoHead + "\n" + strings.TrimSpace(body) + "\n"
}

func render(t Target, version string) string {
	return t.frontMatter + "\n\n" + strings.TrimSpace(body) +
		"\n\n<!-- поставлено hum1izer " + version + " -->\n"
}
