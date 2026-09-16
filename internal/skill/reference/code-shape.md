# hum1izer: the code under the comment

## Complexity of the code the comment sits on

A long comment over a tangled function is a symptom, not a style problem. Before
rewriting such a comment, check the function with what the project already runs:

- `make lint` or `npm run lint` first - the project usually wires this already.
- Go: `golangci-lint run` when `.golangci.yml` is there. Complexity comes from
  `cyclop`, `gocognit`, `nestif`, `funlen`, `dupl`. A linter switched off in the
  config is off on purpose; don't switch it on to win an argument.
- TS and JS: `npx eslint .` when the project has an eslint config. Complexity
  comes from `complexity`, `max-depth`, `max-lines-per-function` and
  `sonarjs/cognitive-complexity`.

Don't install a linter the project doesn't have, and don't score complexity by
eye. hum1izer measures none of this: it reads text, not syntax trees.

If the function trips one of those linters, the fix is the function, not a
better comment. Split it, name the parts, and the comment gets shorter on its
own - often it disappears. The project's own numbers win: a repo that sets
`min-complexity: 30` and `funlen: 240` has made that call, and reporting its
functions against a default is noise.

If the project runs no linter at all, these are the thresholds to fall back on.
They come from a measurement over 33 corpora, 7 languages, 587k functions and
13.5M lines: the Linux kernel, PostgreSQL, sqlite, coreutils, Go stdlib, CPython,
tokio, ripgrep, the Eclipse platform and JDT, DLTK and several working
repositories.

- **Nesting depth at most 4, aim for 3.** This is the one number that held
  everywhere. Median depth is 1 in every corpus measured, across C, Go, Java,
  TypeScript, Swift, Python and Rust. Human code breaks it in 0.1-2.3% of
  functions, so a hit is a signal rather than background. In repositories with
  25 years of history the share does not move: eclipse.platform grew 34x, from
  1537 functions to 51956, and went from 1.11% to 1.09%. Linux CodingStyle puts
  it in words: "if you need more than 3 levels of indentation, you're screwed
  anyway, and should fix your program". Count nested statements, not indent
  levels: measured by indentation the same threshold is 5, because gofmt gives
  `switch` an extra level.
- **Function at most 60 significant lines.** This is where generated code
  actually drifts. Rust written by agents runs 45-50 lines at p95 against 29-34
  for tokio and ripgrep, with three times as many functions over 60 lines -
  while its nesting stays in the human range. Length is the symptom to watch,
  depth is the one to enforce.
- **At most 5 parameters.** If meeting the line or parameter limit would mean
  extracting a single-use helper, the project's rules about abstractions win:
  keep the longer function.
- **Don't use cyclomatic complexity as a threshold.** The usual "under 10" is
  broken by 11.9% of functions in the Go standard library and 13.0% in
  PostgreSQL, and it drifts
  with age on its own: over 25 years eclipse.platform doubled its share while
  nesting stood still. Worse, the only way to lower it is to split a function
  out - which collides with the rule against helpers for single-use operations.
  Depth has no such conflict: it drops with early return and guard clauses,
  that is by deleting code.

In C the rule earns its keep more than anywhere else. With no exceptions, early
exit through `goto` and error checks branching at every call, nesting climbs on
its own: projects that write the rule down sit at 0.4-0.9% (Linux), those that
don't at 2.8-7.3% (PostgreSQL, sqlite, coreutils) - and PostgreSQL has 59
committers over 30 years, so review alone doesn't hold it.

## Writing the code, not just the comment

A comment that needs three paragraphs usually sits on code that needed a
different shape. When you do touch the code, these are the rules that keep it
reading like a person wrote it. They are the other half of the same job: the
measurement above found that repositories written under them carry four times
fewer trivial one-line wrappers than the Go standard library, at a nesting
depth on par with the best corpora in the sample.

**Reach for what exists before writing anything.** In order: a helper already in
this codebase, the standard library, a native platform feature, a dependency
already installed, your own code last. Grep before you write - re-implementing
what lives two files over is the most common kind of slop there is.

**Write less.**

- The simplest thing that works. No speculative features, no future-proofing.
- No abstraction with one implementation: no interface for one type, no factory
  for one product, no config for a value that never changes.
- No helper for a single-use operation. Three similar lines beat a premature
  abstraction.
- No error handling for cases that cannot happen.
- Deletion over addition. Boring over clever - clever is what somebody decodes
  at 3am.
- Fewest files possible.

**Fix the cause, not the symptom.** A bug report names a symptom. Grep every
caller before editing: one guard in the shared function is a smaller diff than a
guard in each caller, and patching only the path from the ticket leaves every
sibling still broken. The smallest diff is right only after you have traced the
whole flow - the smallest change in the wrong place is a second bug.

**Comments: one or two lines, and only where the code cannot say it itself.**
Don't add docstrings, comments, or type annotations to code you didn't change.
Write the why, never the what - the reader can see the what. A deliberate
simplification with a known ceiling is the exception worth a line: name the
ceiling and the way out (`// ponytail: global lock, per-account locks if
throughput matters`).

Never simplify away: input validation at trust boundaries, error handling that
prevents data loss, security measures, accessibility basics, or anything the
person explicitly asked for.

