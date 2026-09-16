package cli

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/haiodo/hum1izer/internal/code"
	"github.com/haiodo/hum1izer/internal/config"
)

const calibrateUsage = `hum1izer calibrate - tune thresholds for this repo.

  hum1izer calibrate [path]        show the spread and cost of each threshold
  hum1izer calibrate --write ...   write the chosen values to .hum1izer.yaml

  --max-lines N  which line threshold to write
  --max-line N   which line length threshold to write
  --write        write; without it, only shows

Comment length norms differ across projects: in the Linux kernel a ten-line
block before a function is documentation, on the web the same block is slop.
The spread across the repo shows what's normal here, the decision stays with
the human.

Only structural thresholds are calibrated. Wordlist rules aren't: they catch
deviation from human norms, and tuning them to a repo full of machine-written
text would just legitimize it.
`

func runCalibrate(args []string) int {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, calibrateUsage) }
	write := fs.Bool("write", false, "write the chosen thresholds to .hum1izer.yaml")
	wantLines := fs.Int("max-lines", 0, "which line threshold to write")
	wantLine := fs.Int("max-line", 0, "which line length threshold to write")
	cfgPath := fs.String("config", "", "settings file instead of looking up .hum1izer.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	cfg, err := loadConfig(*cfgPath, false, root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "settings: %v\n", err)
		return 2
	}

	if *write {
		return writeLimits(cfg, *wantLines, *wantLine)
	}

	paths, err := walkPaths(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	prose, long, files := shapes(paths)
	if len(prose) == 0 {
		fmt.Println("no comments found")
		return 0
	}
	report(cfg, prose, long, files)
	return 0
}

// shapes собирает форму каждого комментария. Пул воркеров тот же, что и у
// прогона: на ядре Linux это полтора миллиона блоков.
func shapes(paths []string) (prose, long []int, files int) {
	workers := max(min(runtime.NumCPU(), len(paths)), 1)
	type part struct {
		prose, long []int
		files       int
	}
	parts := make([]part, workers)
	next := make(chan int, len(paths))
	for i := range paths {
		next <- i
	}
	close(next)

	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range next {
				cmts, err := code.ExtractComments(paths[i])
				if err != nil {
					continue
				}
				parts[w].files++
				for _, c := range cmts {
					if code.Ignored(c) {
						continue
					}
					p, l := code.Shape(c)
					if p == 0 {
						continue
					}
					parts[w].prose = append(parts[w].prose, p)
					parts[w].long = append(parts[w].long, l)
				}
			}
		}(w)
	}
	wg.Wait()

	for _, p := range parts {
		prose = append(prose, p.prose...)
		long = append(long, p.long...)
		files += p.files
	}
	sort.Ints(prose)
	sort.Ints(long)
	return prose, long, files
}

func report(cfg config.Config, prose, long []int, files int) {
	n := len(prose)
	fmt.Printf("comments: %d, files: %d\n", n, files)

	fmt.Println("\nprose lines per comment")
	hist(prose, []int{1, 2, 3, 5, 10, 20})
	fmt.Printf("  median %d, p90 %d, p95 %d, p99 %d, max %d\n",
		pct(prose, 50), pct(prose, 90), pct(prose, 95), pct(prose, 99), prose[n-1])

	fmt.Println("\ncost of the max_lines threshold")
	cost(prose, []int{1, 2, 3, 5, 10, 20}, cur(cfg.Comments.MaxLines, 2))

	fmt.Println("\ncomment line length")
	fmt.Printf("  median %d, p90 %d, p95 %d, p99 %d, max %d\n",
		pct(long, 50), pct(long, 90), pct(long, 95), pct(long, 99), long[len(long)-1])
	fmt.Println("\ncost of the max_line threshold")
	cost(long, []int{60, 80, 100, 120, 160}, cur(cfg.Comments.MaxLine, 100))

	fmt.Printf("\nsuggestion: max_lines %d (p90), max_line %d (p95)\n", pct(prose, 90), pct(long, 95))
	fmt.Printf("write: hum1izer calibrate --write --max-lines %d --max-line %d\n",
		pct(prose, 90), pct(long, 95))
}

// hist - сколько комментариев попало в каждую корзину. Корзины задаются
// верхними границами, последняя открыта вправо.
func hist(sorted []int, edges []int) {
	n, prev := len(sorted), 0
	for _, e := range edges {
		c := upTo(sorted, e) - upTo(sorted, prev)
		label := fmt.Sprint(e)
		if e-prev > 1 {
			label = fmt.Sprintf("%d-%d", prev+1, e)
		}
		bar(label, c, n)
		prev = e
	}
	bar(fmt.Sprintf("%d+", prev+1), n-upTo(sorted, prev), n)
}

// cost: сколько комментариев станут находкой при таком пределе.
func cost(sorted []int, limits []int, current int) {
	n := len(sorted)
	for _, l := range limits {
		over := n - upTo(sorted, l)
		mark := ""
		if l == current {
			mark = "  <- current"
		}
		fmt.Printf("  %4d  %7d  %5.1f%%%s\n", l, over, float64(over)*100/float64(n), mark)
	}
}

func bar(label string, c, n int) {
	share := float64(c) * 100 / float64(n)
	fmt.Printf("  %-7s %7d  %5.1f%%  %s\n", label, c, share, strings.Repeat("#", int(share/3)))
}

// upTo - сколько значений не больше v. Список отсортирован.
func upTo(sorted []int, v int) int {
	return sort.SearchInts(sorted, v+1)
}

func pct(sorted []int, p int) int {
	if len(sorted) == 0 {
		return 0
	}
	i := min(len(sorted)*p/100, len(sorted)-1)
	return sorted[i]
}

func cur(v *int, def int) int {
	if v != nil {
		return *v
	}
	return def
}

func writeLimits(cfg config.Config, lines, line int) int {
	if cfg.Path == "" {
		fmt.Fprintln(os.Stderr, "no settings, create them: hum1izer init")
		return 2
	}
	kv := map[string]any{}
	if lines > 0 {
		kv["comments.max_lines"] = lines
	}
	if line > 0 {
		kv["comments.max_line"] = line
	}
	if len(kv) == 0 {
		fmt.Fprintln(os.Stderr, "nothing to write: give --max-lines or --max-line")
		return 2
	}
	if err := config.Patch(cfg.Path, kv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("written to %s: %v\n", cfg.Path, kv)
	return 0
}
