A comment answers "why". The code already answers "what". Every comment you
write in this repository follows the list below; the CLI is for what is already
in the files.

- One or two lines. Doesn't fit - the comment is unneeded, or the code needs a
  different shape.
- Don't restate the code or the function name. Delete such a line rather than
  rewrite it.
- Don't comment code you didn't change. No docstrings added on the way past.
- No `=====` banners, no tool tags: `ponytail:`, `caveman:`, `claude:`, `AI:`.
  Keep the thought, drop the tag.
- Commented-out code doesn't get committed - git remembers it.
- A `TODO` carries a ticket or a name, or it isn't written.
- Match the tone and vocabulary of the comments already in the file. Don't
  align someone else's style to yours.
- A deliberate simplification with a known ceiling is worth a line: name the
  ceiling and the way out.
- No tutorial tone, no "Note that", no importance instead of fact.

**Fact lock.** You don't know why code was written unless it follows from the
code. Don't invent a reason, a constraint, a ticket number, a name, a deadline.
Don't know - write nothing. An invented explanation is worse than a missing one.

**Never touch:** license headers and SPDX, `//go:generate`, `//go:embed`,
`//nolint`, `// Code generated ... DO NOT EDIT`, `eslint-disable`,
`@ts-expect-error`, compiler pragmas, ticket and commit references.

Editing works by deletion: under the shell there is usually a normal fact, and
it needs freeing, not replacing.
