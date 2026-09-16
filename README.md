# hum1izer

[Русская версия](README.ru.md)

A Go CLI: it checks prose, code comments, and commit messages for traces of AI
generation, officialese, and stock phrases, shows findings with line numbers,
and says what to do with each one. Russian and English.

It only counts what can be caught with grep and arithmetic: 21 hard bans, 21
marker categories, rhythm, nominalization, document structure, a cleanliness
score of 0-100.

The tool writes no prose of its own. Findings can be worked through by hand -
`hum1izer tui ./src` opens three panes with the code around every block - and if
an OpenAI-compatible endpoint is configured, that screen can ask a model to
rewrite a block. The answer is shown as a diff and reaches the file only when
you press `y`.

This is not what the word humanizer usually sells: there is no goal of passing a
detector here. Findings go to the author, the decision stays with a person.

The idea and the initial rule sets are from
[ilyautov/humanizer-ru](https://github.com/ilyautov/humanizer-ru) (MIT, (c) Ilya Utov);
the corpus-based justification for the thresholds is there too: AINL-Eval, LLMTrace, M4-ru.
The engine here is its own, the rules are moved out into YAML and extended.

How the tool was built and what the corpus measurement showed is in the post
[Длинное тире не выдаёт нейросеть. Я проверил на корпусе](https://haiodo.github.io/posts/013_hum1izer)
(in Russian).

## Installation

A prebuilt binary for macOS, Linux, and Windows is in the
[releases](https://github.com/haiodo/hum1izer/releases): download the archive
for your platform, unpack it, put it in `PATH`.

```bash
go install github.com/haiodo/hum1izer@latest   # из исходников
go run github.com/haiodo/hum1izer@latest --code ./src   # без установки
make build                                     # локальная сборка
make install                                   # в GOBIN
```

There is an entry point both at the module root and in `cmd/hum1izer`, so
both forms of the path work. `go.mod` requires Go 1.26, so as not to pull in
the toolchain for those on the previous version.

One dependency - `gopkg.in/yaml.v3` for reading the rules, everything else is
the standard library.

Updating in place:

```bash
hum1izer upgrade           # download the latest release from GitHub and replace the binary
hum1izer upgrade --check   # only check whether a newer one exists
```

The archive is taken from the release for the current GOOS and GOARCH, its sum
is checked against `SHA256SUMS` from the same release, and only then is the new
file renamed over the old one. If the sum does not match, nothing changes.

## Skills for agents

```bash
hum1izer install --claude --codex --opencode --hermes --pi
hum1izer install --all              # все сразу
hum1izer install --claude --dir .   # в проект, а не в домашний каталог
hum1izer install --claude --hooks   # plus the Claude Code hooks
hum1izer install --print            # print SKILL.md without installing it
hum1izer install --repo             # SKILL.md for the repository root (make skill)
make skills                         # собрать и поставить всем
```

`--hooks` wires the same two things for every agent that can take them: the
rule sheet at the start of a session, and a check of the file the agent has just
edited. Only that file is checked, never the tree, and it stays quiet when there
is nothing.

| Agent | What `--hooks` writes |
|---|---|
| `--claude` | `~/.claude/settings.json`, keys `hooks.SessionStart` and `hooks.PostToolUse` |
| `--zcode` | `~/.zcode/cli/config.json`, keys `hooks.events.*` plus `hooks.enabled` |
| `--codex` | `~/.codex/hooks.json`, the same event format |
| `--opencode` | plugin `~/.config/opencode/plugin/hum1izer.js` |
| `--pi` | extension `~/.pi/agent/extensions/hum1izer.ts` |

Hermes and the generic `--agents` target have nothing to hook into; there
`--hooks` says so and installs nothing.

The format is shared - [Agent Skills](https://agentskills.io); only the
header and the path differ:

| Flag | Where |
|---|---|
| `--claude` | `~/.claude/skills/hum1izer/SKILL.md` |
| `--zcode` | `~/.zcode/skills/hum1izer/SKILL.md` |
| `--codex` | `~/.codex/skills/hum1izer/SKILL.md` |
| `--opencode` | `~/.config/opencode/skill/hum1izer/SKILL.md` |
| `--hermes` | `~/.hermes/skills/devops/hum1izer/SKILL.md` |
| `--pi` | `~/.pi/agent/skills/hum1izer/SKILL.md` |
| `--agents` | `~/.agents/skills/hum1izer/SKILL.md` |

opencode also reads `~/.claude/skills` and `~/.agents/skills`, so `--claude`
or `--agents` is enough for it - a separate flag is only needed if you want
to keep the skill in its own directory.

An existing file is not overwritten without `--force`. The skill describes
the editing cycle, the rules for a good comment, and the list of what must
not be touched: license headers, `//go:generate`, `//nolint`,
`eslint-disable`, pragmas.

## Usage

```bash
hum1izer text.md                       # report with recommendations
hum1izer --lang en post.md             # English rule set
hum1izer --genre academic paper.md     # drop markers that are fine for the register
hum1izer --quiet docs/*.md             # one line per file
hum1izer --json text.md                # machine-readable output
cat draft.txt | hum1izer -             # from stdin

hum1izer --code ./src                  # comments in code
hum1izer --code .                      # plus the last 20 commit messages
hum1izer --code --langs go ./src       # one language only
hum1izer --code --only "TODO with no owner" .   # one rule only

hum1izer tui ./src                     # work through the findings by hand
hum1izer calibrate .                   # fit the limits to this repository
hum1izer llm --key <key>               # model used for rewriting
```

A directory prints a summary by rule without the findings themselves: on a tree
there are thousands of them. Name a file and the findings are printed in full.

| Flag | What it does |
|---|---|
| `--genre` | `marketing` (default), `academic`, `legal`, `fiction`, `news` |
| `--lang` | rule set for prose: `ru` (default) or `en` |
| `--format` | `text` (default), `jsonl`, `json`, `quiet` |
| `--limit N` | how many blocks to output, `0` - all. Dirtiest ones come first |
| `--json` | same as `--format json` |
| `--top N` | how many soft markers to show, `0` - all (default 12) |
| `--quiet` | only the final line per file |
| `--rules` | your own rules file instead of the built-in one |
| `--code` | parse the arguments as source code, not as prose |
| `--commits N` | how many recent commits to check, `0` - don't check (20) |
| `--max-lines N` | a comment longer than this many lines is a finding, `0` - don't count (2) |
| `--max-line N` | a comment line longer than this many characters is a finding, `0` - don't count (100) |
| `--skip-tests` | skip `_test.go`, `*.test.*`, `*.spec.*` |
| `--only` | keep only these rules or categories, comma separated |
| `--langs` | scan only these languages: `c`, `cpp`, `go`, `ts`, `js`, `svelte`, `swift`, `java`, `kotlin` |
| `--config F` | use this settings file instead of searching for `.hum1izer.yaml` |
| `--no-config` | ignore `.hum1izer.yaml` |
| `--baseline F` | check against the snapshot, report only what's new |
| `--write-baseline` | overwrite the snapshot with current findings |

Exit code: `0` clean, `1` hard bans found, `2` error. Works for pre-commit
and CI.

## What counts

**Hard bans** - constructs from the "Hard bans" table: `в современном мире`,
`данный`, `стоит отметить`, `комплексный подход`, an em dash, and so on.
Each finding comes with a recommendation from the same table.

**Markers** - lexical categories from the catalog: officialese, calques,
inflated significance, formulaic conclusions, chatbot artifacts, motivational
cliches, marketing stock phrases, emoji decoration, character substitution,
and about a dozen more. One recommendation per category.

**Rhythm** - coefficient of variation of sentence lengths. In human text it
is usually above 0.45; generated text is more even. Plus dashes, ellipses,
parentheses, questions.

**Nominalization** - density of deverbal nouns per 100 words.

**Structure** - spread of paragraph lengths, share of list items (listicle
signature), Title Case in headings, text cut off mid-word.

For `.md`, `.mdx`, and `.markdown`, markup is blanked out before parsing: the
YAML front matter, code blocks, inline code, MDX imports, HTML and JSX tags,
link addresses, table rows. It is blanked out with spaces, so line numbers
and finding positions stay correct. Without this, a code block without a
single period turns into one 1500-word sentence, and rhythm cannot be
measured at all: on a blog post, the CV dropped from 5.04 to 0.82 once the
code was cut out.

**Score** - aggregate of everything above, 0-100. Bands: `≥85` clean,
`60-84` spot edits, `<60` rewrite.

The score reflects the scanner's rules, not an AI probability or a verdict on
authorship. It exists to compare "before and after" on the same text.

Marker density is measured per 100 words, not by raw count: seven markers
per thousand words and seven per hundred are different things, and an
absolute count on a long text always produced "AI with high probability".

## Genres

Without a genre the mode is strict; the original thresholds (marketing,
blogs) are calibrated for it. For academic, legal, fiction, and news text,
some bans are normal for the register rather than a sign of a machine:
`--genre` lifts them.

```
$ hum1izer --quiet ai-sample.txt
ai-sample.txt                             18/100 [rewrite] bans: 6 (+0 dashes), markers: 14.0/100 words

$ hum1izer --genre academic --quiet ai-sample.txt
ai-sample.txt                             46/100 [rewrite] bans: 3 (+0 dashes), markers: 7.5/100 words
```

The report text itself is in English regardless of `--lang`.

## Code comments and commits

```bash
hum1izer --code ./repos/platform
```

The list of files in a git repository is taken from git itself
(`git ls-files --cached --others --exclude-standard`), so `.gitignore` is
honored precisely, including nested and global rules: built code in `lib/`
and `bundle/` is not checked. Outside git, a regular directory walk is used,
skipping `node_modules`, `vendor`, `dist`, and similar.

Generated files are also skipped (`Code generated ... DO NOT EDIT`,
`@generated`, `sourceMappingURL` at the end of the file) as well as license
headers: `SPDX` anywhere, `Copyright` or `Licensed under` in the first five
lines of a file.

`.c`, `.h`, `.cc`, `.cpp`, `.cxx`, `.hh`, `.hpp`, `.go`, `.ts`, `.tsx`, `.js`,
`.jsx`, `.mjs`, `.cjs`, `.svelte`, `.swift`, `.java`, `.kt`, `.kts` are parsed. Go is read through `go/parser`; the rest
through a character-by-character scanner that knows about strings and
templates, so a `//` inside a string is not taken for a comment. The scanner
is split by language: regex literals only in JS and TS, triple quotes in Java
and Kotlin, nested block comments in Kotlin. Block boundaries are taken by
byte offsets, not by lines: for a trailing comment `x := 1 // счётчик`, only
the comment itself goes into `raw`, and the code checked for restatement is
what stands before it on the same line.

Commented-out code is detected not by regex but by parsing: the comment body
is handed to a real parser, and if it parses, it's code. For `.go` this is
`go/parser` from the standard library, the same as go-critic does. For
`.ts`, `.tsx`, `.js`, `.jsx`, and `.svelte` -
[esbuild](https://github.com/evanw/esbuild) (MIT). For Swift there is no
parser, so it still falls back to the share of lines that look like code.

The difference is noticeable. On a platform of 6942 files, the regex found
76 pieces of commented-out code; the parser finds 187, including things like
`// export let attributeKey: string | undefined`,
`// createAction(builder, {...})` - real dead code that the regex did not
recognize.

The cost is size: the binary grows from 3.8 to 8.9 MB. Speed does not
change - 12 seconds for the same 6942 files.

Why not `typescript-go`: its parser, scanner, and AST live under
`internal/`, so they cannot be imported. The only way around it is
vendoring - putting the source inside your own tree and writing an adapter
next to it. A git submodule does not help: `typescript-go` has its own
`go.mod`, and a directory with its own `go.mod` is treated by the compiler
as a separate module, so the `internal` rule kicks in again. The cost of
vendoring is about 1.4 MB of someone else's source (parser, ast, core,
scanner) plus forking an archived repository forever. esbuild gives the same
result with one `go get` line. In `.svelte`, `<script>`, `<style>`, and
`<!-- -->` are taken separately. Adjacent lines are merged into one block: a
header of ten `//` lines is one piece of text, not ten findings. Generated
files (`Code generated ... DO NOT EDIT`, `@generated`), `node_modules`,
`vendor`, `dist`, `gen` are skipped.

The language of each block is determined from the block itself: 30% or more
Cyrillic - the Russian rule set, otherwise English. `rules-code.yaml` is
always applied on top of the language rule set.

Block collection grew out of two in-house tools, `tools/longcomments` and
`scripts/find-long-comments.{go,mjs}`: from there came the Go parsing via
`go/parser`, the character-by-character scanner for TS, and merging adjacent
comments.

If the argument is a git repository, the last `--commits N` commit messages
are included too: the same prose parsing plus header-form checks.

What is checked in addition to the prose rules:

| Finding | Why |
|---|---|
| Comment restates the code | `// set user name` above `setUserName()` carries no information |
| Commented-out code | code history lives in git |
| Comment restates the code | the comment repeats the name next to it, including a trailing comment |
| Long comment | longer than `--max-lines` lines of prose, default 2 |
Two lines by default is a position, not an oversight: a comment answers "why",
and when the "why" does not fit, the explanation belongs in documentation next
to the code. Only prose counts - blank lines and tag lines (`@param`,
`@returns`) are not counted, and package documentation (`package ...` on the
next line) is exempt. A project with another convention sets `max_lines` in
`.hum1izer.yaml`.
| Separator banner | `// =========` should separate by files, not by lines |
| TODO with no owner | without a name or a task reference it's a TODO forever |
| Changelog in a comment | "Updated X to Y", "Author:", a date - that's what git blame is for |
| Markdown essay | headings and lists inside a comment |
| Step-by-step instructions | "Step 1, Step 2" duplicates the code itself |
| Empty comment | `// constructor`, `// imports`, `// инициализация` |
| Empty importance | "ensures that", "отвечает за", "под капотом" |
| Tutorial tone | "as you can see", "давайте", "теперь мы" |
| Apology | "почему-то", "костыль", "hopefully", "not sure why" |
| Promise comment | "временно", "for now", "will be removed" |
| Emoji | show up as noise in diffs and in grep |
| Tool tag | `ponytail:`, `caveman:`, `claude:`, `AI:` - traces of a plugin or model |
| Long line | merging three lines into one 140-character line is not the same as shortening it |

For commits, in addition to this: a header longer than 72 characters, a
period at the end of the header, a header like "Update" or "Fix", an AI
signature in the trailer, "This commit adds ...", a list of five or more
items.

A dash in comments is not counted by default (the `code` genre suppresses
it): there are so many of them that the report turns into a list of dashes.
Fix typography in a separate pass without `--code`.

## Project settings

`.hum1izer.yaml` is searched for starting from the checked directory upward
to the filesystem root, so from any subdirectory of a monorepo the settings
at its root are found. Command-line flags override the file; `--config F`
takes a specific file, `--no-config` ignores any of it.

```bash
hum1izer init            # просканировать и записать .hum1izer.yaml
hum1izer init --print    # посмотреть, ничего не записывая
hum1izer init --force    # перезаписать существующий
```

`init` runs the regular check, writes the detected languages into the file,
and shows as a list of comments which rules are noisiest in this project -
all that's left is to move the excess into `rules.disable`.

```yaml
version: 1

comments:
  max_lines: 2      # комментарий длиннее скольких строк - находка, 0 отключает
  commits: 20       # сколько последних коммитов проверять, 0 отключает
  skip_tests: false

languages:
  only: []          # c, cpp, go, ts, js, svelte, swift, java, kotlin. Пусто - все
  ignore: [swift]

exclude:            # ** проходит через каталоги, * внутри сегмента
  - "**/testdata/**"
  - "docs/legacy/**"

baseline: .hum1izer-baseline   # путь к снимку, считается от файла настроек

rules:
  only: []          # белый список. Пусто - все
  disable:
    - Em dash                 # имя правила
    - Comment shape           # или имя категории целиком

genre: code         # жанр для правил прозы
rules_file: ""      # свой rules.yaml, путь от файла настроек
```

Names in `rules` are taken straight from the report: whatever is printed
after `!` or `*` is what goes into `disable`. A category name works too -
then the whole group is suppressed. The structural-check categories are
`Comment shape` and `Commit shape`.

An unknown key, an unknown language, or a broken glob breaks loading with an
error: a setting that silently doesn't work is worse than no setting at all.

## Snapshot for CI

On a live repository there are thousands of findings, and a red CI from day
one teaches no one anything. So there is a snapshot: a run is compared
against the file and only complains about what's not in it.

```bash
hum1izer --code --baseline .hum1izer-baseline --write-baseline .   # записать
hum1izer --code --baseline .hum1izer-baseline .                    # проверить
```

The file is committed to git. One line per comment, sorted by hash, with the
rule codes spelled out in the header:

```
# hum1izer baseline v3
#
# 073a77 Long comment line
# c3a286 TODO with no owner
#
55dc2ad977ba	c3a286	073a77
```

The key is a hash of the comment text, not the line number: code can be
moved and re-indented, and the finding stays the same. There is no path in the
snapshot, and rules are stored as codes rather than names: on a repository with
two thousand findings that is 35 KB instead of 283. The flip side is that a copy
of the same comment in a new file counts as known. Snapshots in v1 and v2 format
are still read. Edit the comment and
it counts as new - which is correct: if you touched it, clean it up.

stderr prints `в базе 1200, новых 0, исправлено 7` - progress is visible,
and nobody can quietly roll it back.

The exit code with `--baseline` is set for new findings, not for hard ones.
Commits do not go into the snapshot: every new commit would be a new
finding. If `--commits` is not set explicitly, it is turned off when working
with a snapshot.

## Format for an agent

```bash
hum1izer --code --format md --limit 20 ./src
```

One block per comment: a heading, a list of remarks, the text itself in a
fenced block byte for byte, and a ready-to-run fix command. The fence is chosen
longer than the longest run of backticks inside it, so a comment containing a
code block doesn't break the markup.

```markdown
## src/a.ts:5-14
block 55dc2ad977ba | `ts` | comment | weight 7

- **this function is responsible for** (line 6)
  - Say why it's done this way, not what the line below already does

...comment text...

hum1izer fix --block 55dc2ad977ba --text "<new text>" --write src/a.ts
```

The command comes with the id and the path already filled in, so the model does
not have to connect them itself. Where nothing has to be decided - commented-out
code, an empty shell, a banner - `--delete` is filled in instead.

For machine processing there is `--format jsonl`, one line of JSON per
block. It suits an agent worse: in JSON the text is escaped, and a real
comment looks like `[id=\"app-recruit\\\\:string\\"]` - to replace it with an
exact match, the model would have to unescape it mentally.

One line of JSON per block, dirtiest first, a summary on stderr:
`remaining=43 shown=20 findings=61 comments=15035 files=4157`.

```json
{"id":"src/a.ts:1-4","file":"src/a.ts","start":1,"end":4,"lang":"ts",
 "kind":"comment","score":7,
 "raw":"/**\n * This function is responsible for handling the request.\n */",
 "findings":[{"rule":"this function is responsible for","line":2,"hard":false,
              "fix":"Say why it's done this way, not what the line below already does"}]}
```

`raw` is the original text byte for byte, including `//` and `/* */`. The
agent edits the whole block and replaces `raw` with an exact match: line
numbers drift after the very first edit, but `raw` does not. Then it reruns
until `remaining` goes down.

One block is one edit, not a separate finding for every rule match.
`--limit` cuts the work into iterations, `score` sets the order.

A finding's weight in `score` is taken from the rule's `lift` field: how
many times more often the marker occurs in machine text than in human text.
The value is rounded and capped at 10, unmeasured rules weigh one, and a hard
finding costs three times as much - up to 30.

The measurement is our own, on the
[LLMTrace](https://huggingface.co/datasets/iitolstykh/LLMTrace_detection)
(Apache-2.0) corpus: 2708 human and 1455 machine texts in Russian, 2056 and
1352 in English. Generators: gpt-4, o3-2025, gigachat, Qwen2.5-72B,
Magistral, command-r, DeepSeek-R1, gemini-2.5-flash. It is recomputed with a
single command:

```bash
go run ./eval/lift --file valid.jsonl --lang ru        # таблица
go run ./eval/lift --file valid.jsonl --lang ru --tsv  # машинный вывод для переноса в rules.yaml
```

What the measurement showed:

| Marker | lift | |
|---|---|---|
| Emoji decoration (ru) | not present in human text at all | a perfect separator |
| seamless(ly) (en) | 21.0 | |
| crucial (en) | 17.7 | |
| Важно понимать/помнить, что (ru) | 15.1 | |
| not only X but also Y (en) | 12.5 | |
| Bureaucratese (ru) | 2.8 | |
| Em dash (en) | 1.7 | in English there is a signal |
| **Em dash (ru)** | **0.94** | more common in human text than in machine text |
| **Latin inside a Cyrillic word** | **0.28** | these are human typos, not a trace of a machine |
| **Tutorial voice (en)** | **0.79** | more common in human text |

Rules with lift below 1.2 get no weight. The Russian dash stays a hard ban
as a matter of typography, but it is not a generation detector, and that is
exactly why the `code` genre suppresses it.

The numbers are domain-dependent: "Является" scores 2.5 for us, 4.5 in a
mixed register, and 0.1 in academic text according to humanizer-ru's
measurements. Genre matters more than weight. Four rules for which our
corpus didn't have enough hits kept the borrowed values from the
humanizer-ru catalog - they are marked with a comment directly in
`rules.yaml`.

## Working through findings by hand

```bash
hum1izer tui ./src
```

Three panes: rules with their counts, files inside the rule, blocks inside the
file. The block under the cursor is shown with the code around it.

`Enter` opens a menu of actions, so there are no keys to memorize. The scope is
resolved as: picked blocks, else selected files, else the whole rule when the
cursor is in the left pane, else the current file.

| Key | What it does |
|---|---|
| `enter` | menu of actions for the current scope |
| `space` | in the files pane marks a file for the batch, in the blocks pane keeps the block |
| `d` | delete the block |
| `t` | note into `todo.md` |
| `e` | open in `$EDITOR` at the right line |
| `c` | settings: length limits and which languages to scan |
| `m` | pick a model |
| `r` | rescan |
| `esc` | clear the selection, or quit when there is none |

Every decision is written to disk at once. An applied block stays in the list
marked `done`, and only the edited file is rescanned, not the tree.

Notes go into `todo.md` next to `.hum1izer.yaml`: the file with its line, your
text, the rules that fired and the comment itself as a quote. The file is
appended to, never rewritten - it is a log across several passes.

### Rewriting with a model

The tool writes no prose of its own. But when an OpenAI-compatible endpoint is
configured, the menu offers a model pass over the whole scope: requests go in a
batch, at most five at a time, and a rate-limit refusal (429) puts the block
back into the queue and waits for the stated delay.

When the batch drains, the review screen opens: a diff of old and new, and a
row of actions - `Accept`, `Reject`, `Delete`, `Ask again`, `Skip` - driven by
arrows and `enter`. The new text is shown exactly as it will land in the file:
same indent, same marker, wrapped at `max_line`.

```bash
hum1izer llm --key <key>      # store the key and pick a model from /v1/models
hum1izer llm --url <url>      # your own server: llama.cpp, vllm, ollama, openrouter
hum1izer llm                  # show what is configured
hum1izer llm --forget         # wipe it
```

The key lives in a user file, not in the repository:
`$XDG_CONFIG_HOME/hum1izer/config.yaml`, otherwise
`~/.config/hum1izer/config.yaml`, mode `0600`. Resolution order: flag, the
project `.hum1izer.yaml`, the user file, then `OPENAI_BASE_URL`,
`OPENAI_MODEL`, `OPENAI_API_KEY`.

## Fitting the limits to a repository

```bash
hum1izer calibrate .
hum1izer calibrate --write --max-lines 2 --max-line 95 .
```

Shows the spread of comment lengths across the repository and the price of every
candidate limit, leaving the decision to a person. Norms differ: in the Linux
kernel the 95th percentile of line length is 69 characters, in a large
TypeScript monorepo it is 95, and one threshold does not fit both.

License headers, pragmas and blocks marked `hum1izer:keep` are excluded from the
measurement - the check does not look at them either.

Only structural limits are calibrated. Vocabulary rules catch deviation from the
human norm, and fitting their threshold to a repository full of machine text
would just legalize it.

## Fixing by hash

The tool not only shows findings, it applies the fix, so that a model doesn't
invent its own way of editing the file:

```bash
hum1izer fix --block 55dc2ad977ba --text "The reason, not a restatement" ./src   # preview
hum1izer fix --block 55dc2ad977ba --text "The reason, not a restatement" --write ./src   # apply
hum1izer fix --block a832aeaac8cf --delete --write ./src                         # delete the block
hum1izer fix --auto ./src                                                        # mechanical only
```

### A round of thirty

The report and the edit share one format, so the loop closes without moving
fields around:

```bash
hum1izer --code --format jsonl --limit 30 ./src > work.jsonl   # 1. take the 30 worst
# 2. add "text": "..." or "delete": true to the lines you change
hum1izer fix --batch work.jsonl --write ./src                   # 3. apply
```

A report line already holds everything the decision needs: `hash` is the block
id, plus `file`, `start`, `end`, `raw` (the comment byte-for-byte) and
`findings` with the rule and its advice. The `block` field is optional in a
batch: the `hash` from the report works as is. Lines with neither `text` nor
`delete` are skipped and counted on stderr. Then run the report again -
`remaining` on stderr is how many blocks are left.

In bulk - one JSON per line, the tree is walked once:

```bash
printf '%s\n' '{"block":"55dc2ad977ba","text":"new text"}' \
               '{"block":"a832aeaac8cf","delete":true}' \
               '{"block":"56fa9eb0ddfa","file":"src/a.ts","delete":true}' |
  hum1izer fix --batch - --write ./src
```

The same text in several files is one block with one id, and the report marks it
`text copies: N`. Without `file` the edit goes to every copy - that is what you
want for a stock line repeated across the tree. With `file` it goes only there.

You give the prose only: the tool restores the marker, the indent and the wrap
at `--max-line`. Deleting removes whole lines and leaves no blanks behind, and
if the line also held code with a trailing comment, the code stays. The block is
addressed by hash, so the edit does not depend on line numbers: if the text has
changed since the report, the hash won't match and the tool refuses instead of
damaging someone else's spot.

`--auto` deletes what needs no judgement: commented-out code and empty shells
like `// constructor`.

### Blocks you decided to keep

Not every finding is worth a fix. `--keep` writes the block into the baseline
instead of editing it, so the next run stays quiet about it:

```bash
hum1izer fix --block 55dc2ad977ba --keep --write ./src      # takes baseline from .hum1izer.yaml
hum1izer fix --block 55dc2ad977ba --keep --baseline .hum1izer-baseline --write ./src
```

In a batch it is `{"block":"55dc2ad977ba","keep":true}` alongside the real
edits. The snapshot holds the text, so changing the comment later brings the
finding back.

When the decision belongs in the code and not in a snapshot - a commented-out
block kept on purpose, a table, a foreign format - put `hum1izer:keep`
anywhere in the comment:

```go
// hum1izer:keep - the old query, still needed when the index is rebuilt
// SELECT id FROM docs WHERE ...
```

The whole block is then skipped, in every mode, with no baseline involved.

## Custom rules

The rules live in three files, all embedded in the binary:

| File | What's in it |
|---|---|
| [internal/humanize/rules.yaml](internal/humanize/rules.yaml) | Russian prose: bans, markers, thresholds, genres |
| [internal/humanize/rules-en.yaml](internal/humanize/rules-en.yaml) | the same for English |
| [internal/humanize/rules-code.yaml](internal/humanize/rules-code.yaml) | comments and commits, on top of the language rule set |

The Russian rule set is replaced with a flag:

```bash
hum1izer --rules my-rules.yaml текст.md
```

Format of a single rule:

```yaml
hard_bans:
  - name: Синергия
    fix: Скажи, что конкретно с чем соединяется
    word_re: 'синерги[\p{L}]+'

categories:
  - name: Мой канцелярит
    fix: Разверни в глагол
    rules:
      - lit: осуществлени          # подстрока, регистронезависимо
      - re: 'посвящен[ао]\s+анализ' # регулярка RE2 как есть
      - word_re: 'важн[\p{L}]+ шаг' # то же, но с границами слова
      - builtin: en_dash            # правило, которое считает код
```

Three things worth remembering when editing:

- in Go, `\b` and `\w` only work on ASCII, so for Cyrillic write `word_re`
  instead of `\b` and `[\p{L}]+` instead of `\w`;
- RE2 has no lookaround or backreferences: whatever can't be expressed
  without them lives in the code as `builtin` (`en_dash`, `fake_range`,
  `latin_in_cyrillic`);
- write invisible characters in the rule set as codes (`\x{200b}`), not as
  literals: they are not visible in a diff, and GitHub complains about
  bidirectional Unicode.

The loader validates the rule set at startup: if a genre suppresses a ban
that doesn't exist, hum1izer fails with an error instead of silently doing
nothing.

## Differences from the original scanner

The port was checked against `scripts/scan.py` on the same text: the score,
the composition of penalties, and the list of findings match. Three
differences in the implementation:

1. **Nominalization.** The original counts nouns/verbs via `pymorphy3`.
   Here there is no dictionary; instead of a ratio, the density of deverbal
   nouns is counted by suffix. The signal is the same (officialese), the
   penalty weight is the same. The ceiling of the heuristic: it doesn't
   distinguish "решение задачи" (solving a problem) from "решение принято"
   (a decision has been made).
2. **Word boundaries and lookaround.** In Go, `\b` and `\w` only work on
   ASCII, and there is no lookaround or backreferences at all. Boundaries
   are set explicitly; three rules (short dash, a false range "from X to
   Y", Latin letters inside Cyrillic) are computed in code.
3. **Line numbers.** More precise than in the original: there, the position
   is taken from the start of the match, so a pattern with a leading
   separator (`. Таким образом,`) points to the line of the previous
   sentence.

The original filters out Title Case headings using a dictionary of proper
names; here there is no dictionary, so a heading made of several names will
produce a false positive.

## What is not here

Semantics: meaning-level calques, irony, translationese, empty imagery,
"figurative zero". This is fundamentally not catchable with regexes; for
that you need the humanizer-ru skill itself, with a model.

There is no rewriting of its own either: the rules only show and advise. The
prose comes from a model, and only when it is called from `tui`; it reaches the
file only after a person has looked at the diff and agreed.

No code complexity either - no cyclomatic or cognitive complexity, no
maintainability index. That is a job for `golangci-lint` (`cyclop`, `gocognit`,
`nestif`, `funlen`, `dupl`) and for eslint (`complexity`, `max-depth`); the
skill tells the agent to run them instead of guessing, and when a project runs
no linter at all it falls back on thresholds measured over 13 corpora - nesting
depth at most 4, function at most 60 lines. hum1izer reads text, not syntax
trees.

## Development

```
cmd/hum1izer/       флаги, вывод, подкоманды install и init
internal/baseline/  снимок находок для CI
eval/lift/          пересчёт весов lift на парном корпусе
internal/config/    .hum1izer.yaml: поиск вверх, языки, исключения, правила
internal/humanize/  правила, разбор прозы, балл + rules*.yaml
internal/code/      комментарии из исходников, коммиты, структурные проверки
internal/skill/     SKILL.md и раскладка по агентам
```

```bash
make check   # fmt + vet + golangci-lint + test
make build   # бинарь с версией из git describe
```

CI runs `go vet`, `gofmt -l`, golangci-lint, and `go test -race` on every
push. A release is built on a `v*` tag: darwin/arm64, darwin/amd64,
linux/amd64, linux/arm64, windows/amd64; the archives and `SHA256SUMS` go
out to the GitHub Release.

```bash
git tag v0.1.0 && git push --tags
```

## License

MIT, (c) 2026 Andrey Sobolev.

The rule sets, thresholds, corpus-based `lift` weights, and the editing
procedure in the skill were originally taken from
[humanizer-ru](https://github.com/ilyautov/humanizer-ru) (MIT, (c) Ilya Utov).
