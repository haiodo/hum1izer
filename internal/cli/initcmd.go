package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/config"
	"github.com/haiodo/hum1izer/internal/humanize"
)

const initUsage = `hum1izer init - create .hum1izer.yaml with project settings.

  hum1izer init [path]     scan and write settings (default .)
  hum1izer init --print    show what would be written, write nothing
  hum1izer init --force    overwrite an existing file

Scanning fills the file with the languages found and flags which rules are
noisiest in this project.
`

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, initUsage) }
	force := fs.Bool("force", false, "overwrite an existing file")
	printOnly := fs.Bool("print", false, "print to stdout, write nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	out := filepath.Join(root, config.Name)
	if _, err := os.Stat(out); err == nil && !*printOnly && !*force {
		fmt.Fprintf(os.Stderr, "%s already exists, --force overwrites it\n", out)
		return 2
	}

	st, err := survey(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", root, err)
		return 2
	}
	body := render(st)
	if *printOnly {
		fmt.Print(body)
		return 0
	}
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	fmt.Printf("%s written: languages %d, files %d, comments %d, findings %d\n",
		out, len(st.langs), st.files, st.comments, st.findings)
	return 0
}

type stats struct {
	langs                     map[string]int
	rules                     map[string]int
	files, comments, findings int
}

// survey гоняет обычную проверку на дефолтных настройках: нужны только
// количества, поэтому отчёт никуда не печатается.
func survey(root string) (stats, error) {
	st := stats{langs: map[string]int{}, rules: map[string]int{}}
	cs := code.CodeSets{MaxLines: 2}
	var err error
	if cs.RU, err = humanize.LoadBuiltin("ru"); err != nil {
		return st, err
	}
	if cs.EN, err = humanize.LoadBuiltin("en"); err != nil {
		return st, err
	}
	if cs.Code, err = humanize.LoadBuiltin("code"); err != nil {
		return st, err
	}

	paths, err := code.WalkCode(root, code.Filter{})
	if err != nil {
		return st, err
	}
	for _, p := range paths {
		st.files++
		st.langs[config.LangOf(p)]++
		cmts, err := code.ExtractComments(p)
		if err != nil {
			continue
		}
		for _, c := range cmts {
			if strings.TrimSpace(c.Text) == "" {
				continue
			}
			st.comments++
			for _, f := range code.CheckComment(cs, c, "code") {
				st.findings++
				st.rules[f.Rule]++
			}
		}
	}
	return st, nil
}

func render(st stats) string {
	var b strings.Builder
	b.WriteString(`# hum1izer settings for this project.
# The file is looked up from the checked directory up to the root, flags override it.

version: 1

comments:
  # Comment longer than this many lines counts as a finding. 0 disables the check.
  max_lines: 2
  # How many recent commits to check when scanning a git repo.
  commits: 20
  skip_tests: false

languages:
`)
	fmt.Fprintf(&b, "  # Found in project: %s\n", langSummary(st.langs))
	b.WriteString(`  # only - allowlist, empty means all supported.
  only: []
  ignore: []

# Paths to skip scanning. ** crosses directories, * stays within a segment.
# node_modules, vendor, dist, gen and generated files are skipped by default.
exclude: []

# Baseline of known findings. Without this line, a run without --baseline
# won't read it, and blocks deferred via fix --keep resurface again.
baseline: .hum1izer-baseline

rules:
  # only - allowlist, empty means all. Name comes from the report: both a
  # rule name and a whole category name work.
  only: []
  disable: []
`)
	if top := topRules(st.rules); len(top) > 0 {
		b.WriteString("\n# Fires most often in this project:\n")
		for _, r := range top {
			fmt.Fprintf(&b, "#   %4d  %s\n", r.n, r.name)
		}
		b.WriteString("# If any of this is normal for the project, move the name to rules.disable.\n")
	}
	return b.String()
}

func langSummary(langs map[string]int) string {
	if len(langs) == 0 {
		return "nothing found"
	}
	keys := make([]string, 0, len(langs))
	for l := range langs {
		keys = append(keys, l)
	}
	sort.Slice(keys, func(i, j int) bool { return langs[keys[i]] > langs[keys[j]] })
	parts := make([]string, 0, len(keys))
	for _, l := range keys {
		parts = append(parts, fmt.Sprintf("%s (%d)", l, langs[l]))
	}
	return strings.Join(parts, ", ")
}

type ruleCount struct {
	name string
	n    int
}

func topRules(m map[string]int) []ruleCount {
	out := make([]ruleCount, 0, len(m))
	for name, n := range m {
		out = append(out, ruleCount{name, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].name < out[j].name
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}
