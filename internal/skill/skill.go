// Package skill раскладывает SKILL.md по каталогам агентов. Формат общий -
// Agent Skills (agentskills.io), различается только шапка и путь установки.
package skill

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed body.md
var body string

//go:embed rules.md
var rules string

//go:embed reference
var reference embed.FS

//go:embed plugins
var plugins embed.FS

// Rules - короткий свод правил для комментариев. Его же печатает хук
// SessionStart, поэтому текст живёт одним файлом, а не двумя копиями.
func Rules() string { return strings.TrimSpace(rules) }

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
	{Flag: "zcode", Agent: "ZCode", Dir: ".zcode/skills/" + name, frontMatter: `---
name: ` + name + `
description: "` + description + `"
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
	{Flag: "agents", Agent: "any agent following the Agent Skills standard", Dir: ".agents/skills/" + name, frontMatter: `---
name: ` + name + `
description: "` + description + `"
license: MIT
---`},
}

const usage = `hum1izer install - install the skill for an agent.

  hum1izer install --claude --codex     selected agents
  hum1izer install --all                all at once
  hum1izer install --claude --dir .     into the project, not the home dir
  hum1izer install --claude --hooks     plus hooks for that agent
  hum1izer install --print              print SKILL.md and exit

Agents:
  --claude   ~/.claude/skills/hum1izer/SKILL.md
  --zcode    ~/.zcode/skills/hum1izer/SKILL.md
  --codex    ~/.codex/skills/hum1izer/SKILL.md
  --opencode ~/.config/opencode/skill/hum1izer/SKILL.md
  --hermes   ~/.hermes/skills/devops/hum1izer/SKILL.md
  --pi       ~/.pi/agent/skills/hum1izer/SKILL.md
  --agents   ~/.agents/skills/hum1izer/SKILL.md
             opencode also reads this directory, and ~/.claude/skills

Other:
  --all      all agents at once
  --dir D    root instead of the home directory
  --force    overwrite if the file already exists
  --hooks    install hooks for the selected agents: rules at session start and
             a check right after editing a file. Claude Code, ZCode and Codex
             get them in settings, opencode and pi as a plugin
  --print    print SKILL.md to stdout
  --repo     print SKILL.md for the repo root (make skill)
`

// Run выполняет подкоманду install. Возвращает код выхода.
func Run(args []string, version string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	picked := map[string]*bool{}
	for _, t := range targets {
		picked[t.Flag] = fs.Bool(t.Flag, false, "install for "+t.Agent)
	}
	all := fs.Bool("all", false, "all agents at once")
	root := fs.String("dir", "", "root instead of the home directory")
	force := fs.Bool("force", false, "overwrite the existing file")
	print := fs.Bool("print", false, "print SKILL.md to stdout")
	repo := fs.Bool("repo", false, "print SKILL.md for the repo root")
	hooks := fs.Bool("hooks", false, "register Claude Code hooks in settings.json")
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
	if len(chosen) == 0 && !*hooks {
		fs.Usage()
		return 2
	}

	base := *root
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "home directory: %v\n", err)
			return 2
		}
		base = home
	}

	exit := 0
	for _, t := range chosen {
		path := filepath.Join(base, filepath.FromSlash(t.Dir), "SKILL.md")
		if _, err := os.Stat(path); err == nil && !*force {
			fmt.Printf("  = %-10s already installed: %s (--force overwrites)\n", t.Flag, path)
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
		if err := writeRef(filepath.Dir(path), *force); err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s reference: %v\n", t.Flag, err)
			exit = 2
		}
		fmt.Printf("  + %-10s %s\n", t.Flag, path)
	}
	if *hooks {
		flags := make([]string, 0, len(chosen))
		for _, t := range chosen {
			flags = append(flags, t.Flag)
		}
		if code := installHooks(base, flags, *force); code != 0 {
			exit = code
		}
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
	return repoHead + "\n" + text() + "\n"
}

func render(t Target, version string) string {
	return t.frontMatter + "\n\n" + text() +
		"\n\n<!-- installed by hum1izer " + version + " -->\n"
}

// text - тело скилла с подставленными правилами: в body.md на их месте стоит
// метка, чтобы свод правил не пришлось держать в двух файлах сразу.
func text() string {
	return strings.Replace(strings.TrimSpace(body), "<!-- rules -->", Rules(), 1)
}

// writeRef раскладывает reference/*.md рядом со SKILL.md. Без них ядро скилла
// ссылается в пустоту.
func writeRef(dir string, force bool) error {
	entries, err := reference.ReadDir("reference")
	if err != nil {
		return err
	}
	for _, e := range entries {
		data, err := reference.ReadFile("reference/" + e.Name())
		if err != nil {
			return err
		}
		path := filepath.Join(dir, "reference", e.Name())
		if _, err := os.Stat(path); err == nil && !force {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
