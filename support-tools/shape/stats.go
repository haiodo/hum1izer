package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// nestKinds - что считается уровнем вложенности. Вложенность важнее
// цикломатики: switch на 40 веток плоский и читается, три вложенных if - нет.
var nestKinds = map[string]bool{
	"if_statement": true, "while_statement": true, "for_statement": true,
	"for_in_statement": true, "for_of_statement": true, "do_statement": true,
	"if_expression": true, "while_expression": true, "for_expression": true,
	"loop_expression": true, "match_expression": true, "with_statement": true,
	"switch_expression": true,
	"switch_statement":  true, "expression_switch_statement": true,
	"type_switch_statement": true, "select_statement": true,
	"try_statement": true, "catch_clause": true, "when_expression": true,
}

type metric struct {
	cc, sloc, depth, params int
}

func scanStats(src []byte, root *ts.Node, skipErr bool) []metric {
	var out []metric
	var deepest func(n *ts.Node, d int) int
	deepest = func(n *ts.Node, d int) int {
		if nestKinds[n.Kind()] {
			d++
		}
		best := d
		for i := uint(0); i < n.ChildCount(); i++ {
			if v := deepest(n.Child(i), d); v > best {
				best = v
			}
		}
		return best
	}
	var body func(n *ts.Node) int
	body = func(n *ts.Node) int {
		cc := 0
		if branch(n, src) {
			cc++
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			cc += body(n.Child(i))
		}
		return cc
	}
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		if funcKinds[n.Kind()] {
			if skipErr && n.HasError() {
				return
			}
			np := params(n)
			out = append(out, metric{1 + body(n), sloc(src[n.StartByte():n.EndByte()]), deepest(n, 0), np})
			return
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return out
}

func p(v []int, q int) int {
	if len(v) == 0 {
		return 0
	}
	return v[min(len(v)*q/100, len(v)-1)]
}

func share(v []int, over int) float64 {
	n := 0
	for _, x := range v {
		if x > over {
			n++
		}
	}
	return float64(n) * 100 / float64(len(v))
}

// listOf - если задан файл со списком путей, берём его вместо обхода дерева.
func listOf(root string) []string {
	b, err := os.ReadFile(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func runStats(label, root string) {
	parsers := map[string]*ts.Parser{}
	var ms []metric
	each := func(pa string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d != nil && d.IsDir() {
			switch d.Name() {
			case "vendor", "testdata", ".git", "node_modules", "build", "dist", "SourcePackages":
				return filepath.SkipDir
			}
			return nil
		}
		lang := langOf(pa)
		if lang == nil || strings.HasSuffix(pa, "_test.go") || strings.Contains(pa, ".test.") ||
			strings.Contains(pa, "__tests__") || strings.HasSuffix(pa, ".d.ts") {
			return nil
		}
		ext := filepath.Ext(pa)
		pr := parsers[ext]
		if pr == nil {
			pr = ts.NewParser()
			pr.SetLanguage(lang)
			parsers[ext] = pr
		}
		src, err := os.ReadFile(pa)
		if err != nil {
			return nil
		}
		tree := pr.Parse(src, nil)
		if tree == nil {
			return nil
		}
		ms = append(ms, scanStats(src, tree.RootNode(), true)...)
		tree.Close()
		return nil
	}
	if list := listOf(root); list != nil {
		for _, f := range list {
			each(f, nil, nil)
		}
	} else {
		filepath.WalkDir(root, each)
	}
	var cc, sl, de, pa []int
	for _, m := range ms {
		cc = append(cc, m.cc)
		sl = append(sl, m.sloc)
		de = append(de, m.depth)
		pa = append(pa, m.params)
	}
	for _, v := range [][]int{cc, sl, de, pa} {
		sort.Ints(v)
	}
	if os.Getenv("CSV") != "" {
		fmt.Printf("%s,%d,%d,%d,%d,%d,%d,%d,%.2f,%.2f,%.2f\n", label, len(ms),
			p(cc, 50), p(cc, 90), p(cc, 95), p(sl, 50), p(sl, 90), p(de, 95),
			share(cc, 10), share(de, 4), share(sl, 60))
		return
	}
	if os.Getenv("CROSS") != "" {
		cross(label, ms)
		return
	}
	if os.Getenv("TINY") != "" {
		var tiny, small int
		for _, m := range ms {
			if m.sloc <= 3 {
				tiny++
			}
			if m.sloc <= 6 {
				small++
			}
		}
		n := float64(len(ms))
		fmt.Printf("%-22s функций %6d  до 3 строк %5.1f%%  до 6 строк %5.1f%%  медиана sloc %2d  медиана cc %d\n",
			label, len(ms), float64(tiny)*100/n, float64(small)*100/n, p(sl, 50), p(cc, 50))
		return
	}
	fmt.Printf("%-22s %6d | cc %2d %3d %3d %3d | sloc %3d %3d %3d %4d | depth %d %d %d %d | par %d %d %d | cc>10 %4.1f%% depth>4 %4.1f%% sloc>60 %4.1f%%\n",
		label, len(ms),
		p(cc, 50), p(cc, 90), p(cc, 95), p(cc, 99),
		p(sl, 50), p(sl, 90), p(sl, 95), p(sl, 99),
		p(de, 50), p(de, 90), p(de, 95), p(de, 99),
		p(pa, 50), p(pa, 90), p(pa, 99),
		share(cc, 10), share(de, 4), share(sl, 60))
}

// params - число параметров. В C список лежит под function_declarator, в Go и
// TS - прямо в узле функции, поэтому ищем parameter_list в обоих местах.
func params(n *ts.Node) int {
	find := func(x *ts.Node) *ts.Node {
		for _, f := range []string{"parameters", "parameter_list"} {
			if p := x.ChildByFieldName(f); p != nil {
				return p
			}
		}
		for i := uint(0); i < x.ChildCount(); i++ {
			k := x.Child(i).Kind()
			if k == "parameter_list" || k == "formal_parameters" || k == "parameters" {
				return x.Child(i)
			}
		}
		return nil
	}
	p := find(n)
	if p == nil {
		if d := n.ChildByFieldName("declarator"); d != nil {
			p = find(d)
		}
	}
	if p == nil {
		return 0
	}
	return int(p.NamedChildCount())
}

// cross - независимы ли глубина и цикломатика. Если они об одном, второй
// критерий агенту не нужен.
func cross(label string, ms []metric) {
	var flat, deep, both, neither int
	for _, m := range ms {
		hc, hd := m.cc > 10, m.depth > 4
		switch {
		case hc && hd:
			both++
		case hc:
			flat++
		case hd:
			deep++
		default:
			neither++
		}
	}
	n := float64(len(ms))
	fmt.Printf("%-16s плоские но сложные (cc>10, depth<=4) %5.1f%%   глубокие но простые (cc<=10, depth>4) %4.1f%%   и то и то %4.1f%%\n",
		label, float64(flat)*100/n, float64(deep)*100/n, float64(both)*100/n)
}
