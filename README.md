# hum1izer

[Русская версия](README.ru.md)

A Go CLI: it checks prose, code comments, and commit messages for traces of AI
generation, officialese, and stock phrases, shows findings with line numbers,
and says what to do with each one. Russian and English.

It only counts what can be caught with grep and arithmetic: 21 hard bans, 19
marker categories, rhythm, nominalization, document structure, a cleanliness
score of 0-100.

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
hum1izer install --print            # посмотреть SKILL.md, ничего не ставя
make skills                         # собрать и поставить всем
```

The format is shared - [Agent Skills](https://agentskills.io); only the
header and the path differ:

| Flag | Where |
|---|---|
| `--claude` | `~/.claude/skills/hum1izer/SKILL.md` |
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
hum1izer текст.md                      # отчёт с рекомендациями
hum1izer --lang en post.md             # английский набор правил
hum1izer --genre academic статья.md    # снять маркеры, законные для регистра
hum1izer --quiet docs/*.md             # одна строка на файл
hum1izer --json текст.md               # машинный вывод
cat draft.txt | hum1izer -             # из stdin

hum1izer --code ./src                  # комментарии в коде
hum1izer --code .                      # то же плюс последние 20 коммитов
```

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
| `--skip-tests` | skip `_test.go`, `*.test.*`, `*.spec.*` |
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
ai-sample.txt   19/100 [рерайт] банов: 14, маркеров: 13

$ hum1izer --genre academic --quiet ai-sample.txt
ai-sample.txt   35/100 [рерайт] банов: 9, маркеров: 5
```

The report text itself is currently Russian regardless of `--lang`.

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

`.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.svelte`, `.swift`,
`.java`, `.kt`, `.kts` are parsed. Go is read through `go/parser`; the rest
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

Block collection grew out of `platform-go/tools/longcomments` and
`platform/scripts/find-long-comments.{go,mjs}`: from there came the Go
parsing via `go/parser`, the character-by-character scanner for TS, and
merging adjacent comments.

If the argument is a git repository, the last `--commits N` commit messages
are included too: the same prose parsing plus header-form checks.

What is checked in addition to the prose rules:

| Finding | Why |
|---|---|
| Restating the code | `// set user name` above `setUserName()` carries no information |
| Commented-out code | code history lives in git |
| Restating the code | the comment repeats the name next to it, including a trailing comment |
| Long comment | longer than `--max-lines` lines, default 2 |
| Banner separator | `// =========` should separate by files, not by lines |
| TODO without an owner | without a name or a task reference it's a TODO forever |
| Changelog in a comment | "Updated X to Y", "Author:", a date - that's what git blame is for |
| Markdown essay | headings and lists inside a comment |
| Step-by-step instructions | "Step 1, Step 2" duplicates the code itself |
| Empty-shell comment | `// constructor`, `// imports`, `// инициализация` |
| Empty gravitas | "ensures that", "отвечает за", "под капотом" |
| Tutorial tone | "as you can see", "давайте", "теперь мы" |
| Apology | "почему-то", "костыль", "hopefully", "not sure why" |
| Comment-as-a-promise | "временно", "for now", "will be removed" |
| Emoji | show up as noise in diffs and in grep |
| Tool marker | `ponytail:`, `caveman:`, `claude:`, `AI:` - traces of a plugin or model |
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
  only: []          # go, ts, js, svelte, swift, java, kotlin. Пусто - все
  ignore: [swift]

exclude:            # ** проходит через каталоги, * внутри сегмента
  - "**/testdata/**"
  - "docs/legacy/**"

baseline: .hum1izer-baseline   # путь к снимку, считается от файла настроек

rules:
  only: []          # белый список. Пусто - все
  disable:
    - Длинное тире            # имя правила
    - Структура комментария   # или имя категории целиком

genre: code         # жанр для правил прозы
rules_file: ""      # свой rules.yaml, путь от файла настроек
```

Names in `rules` are taken straight from the report: whatever is printed
after `!` or `*` is what goes into `disable`. A category name works too -
then the whole group is suppressed. The structural-check categories are
`Структура комментария` and `Форма коммита`.

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

The file is committed to git. It is sorted and stores relative paths so that
diffs are readable:

```
# hum1izer baseline v1
55dc2ad977ba	TODO без владельца	internal/code/check.go
```

The key is a hash of the comment text, not the line number: code can be
moved and re-indented, and the finding stays the same. Edit the comment and
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

One block per comment: a heading, a list of remarks, and the text itself in
a fenced block, byte for byte. The fence is chosen longer than the longest
run of backticks inside it, so a comment containing a code block doesn't
break the markup.

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
              "fix":"Скажи, почему так сделано, а не что делает строка ниже"}]}
```

`raw` is the original text byte for byte, including `//` and `/* */`. The
agent edits the whole block and replaces `raw` with an exact match: line
numbers drift after the very first edit, but `raw` does not. Then it reruns
until `remaining` goes down.

One block is one edit, not a separate finding for every rule match.
`--limit` cuts the work into iterations, `score` sets the order.

A finding's weight in `score` is taken from the rule's `lift` field: how
many times more often the marker occurs in machine text than in human text.
A hard finding gets triple weight, capped at 10; unmeasured rules have a
weight of one.

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
| Эмодзи-декор (ru) | not present in human text at all | a perfect separator |
| seamless(ly) (en) | 21.0 | |
| crucial (en) | 17.7 | |
| Важно понимать/помнить, что (ru) | 15.1 | |
| not only X but also Y (en) | 12.5 | |
| Канцелярит (ru) | 2.8 | |
| Em dash (en) | 1.7 | in English there is a signal |
| **Длинное тире (ru)** | **0.94** | more common in human text than in machine text |
| **Латиница внутри кириллицы** | **0.28** | these are human typos, not a trace of a machine |
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

There is no text rewriting either: hum1izer only shows and advises.

No code complexity either - no cyclomatic or cognitive complexity, no
maintainability index. That is a job for `golangci-lint` (`cyclop`, `gocognit`,
`nestif`, `funlen`, `dupl`) and for eslint (`complexity`, `max-depth`); the
skill tells the agent to run them instead of guessing. hum1izer reads text, not
syntax trees.

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
