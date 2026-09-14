package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// funcKinds - узлы, которые считаем функцией. Имена совпадают между
// грамматиками не полностью, поэтому список явный.
var funcKinds = map[string]bool{
	"function_declaration": true, "method_declaration": true, "func_literal": true,
	"function_definition": true, "method_definition": true, "constructor_declaration": true,
	"arrow_function": true, "function_expression": true, "function": true,
	"lambda_expression": true, "function_item": true, "local_function_statement": true,
}

// branchKinds - узлы, дающие +1 к цикломатике. Ветвление на case разбирается
// отдельно: default веткой не считается.
var branchKinds = map[string]bool{
	"if_statement": true, "while_statement": true, "for_statement": true,
	"for_in_statement": true, "for_of_statement": true, "do_statement": true,
	"catch_clause": true, "conditional_expression": true, "ternary_expression": true,
	"guard_statement": true, "range_clause": true,
	"if_expression": true, "while_expression": true, "for_expression": true,
	"loop_expression": true, "match_arm": true, "elif_clause": true,
	"except_clause": true, "switch_section": true,
	"expression_case": true, "type_case": true, "communication_case": true,
	"switch_block_statement_group": true,
}

func branch(n *ts.Node, src []byte) bool {
	k := n.Kind()
	if branchKinds[k] {
		return true
	}
	if k == "case_statement" || k == "switch_case" {
		return n.ChildByFieldName("value") != nil
	}
	if k == "binary_expression" {
		if op := n.ChildByFieldName("operator"); op != nil {
			t := string(src[op.StartByte():op.EndByte()])
			return t == "&&" || t == "||"
		}
	}
	return false
}

type tfn struct {
	name string
	cc   int
	sloc int
}

// scan обходит дерево один раз: функции верхнего уровня, внутри них ветвления.
// Вложенная лямбда идёт в цикломатику родителя, как у gocyclo.
var skipped int

func scan(path string, src []byte, root *ts.Node) []tfn {
	var out []tfn
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
			if os.Getenv("CLEAN") != "" && n.HasError() {
				skipped++
				return
			}
			name := "?"
			if id := n.ChildByFieldName("name"); id != nil {
				name = string(src[id.StartByte():id.EndByte()])
			} else if d := n.ChildByFieldName("declarator"); d != nil {
				name = strings.SplitN(string(src[d.StartByte():d.EndByte()]), "(", 2)[0]
			}
			lines := sloc(src[n.StartByte():n.EndByte()])
			out = append(out, tfn{path + ":" + name, 1 + body(n), lines})
			return // вложенные функции уже вошли в эту
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return out
}

func runCC(roots []string) {
	var fns []tfn
	parsers := map[string]*ts.Parser{}
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case "vendor", "testdata", ".git", "node_modules", "build", "SourcePackages", "dist":
					return filepath.SkipDir
				}
				return nil
			}
			lang := langOf(p)
			if lang == nil || strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, ".d.ts") {
				return nil
			}
			ext := filepath.Ext(p)
			pr := parsers[ext]
			if pr == nil {
				pr = ts.NewParser()
				pr.SetLanguage(lang)
				parsers[ext] = pr
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			tree := pr.Parse(src, nil)
			if tree == nil {
				return nil
			}
			fns = append(fns, scan(p, src, tree.RootNode())...)
			tree.Close()
			return nil
		})
	}
	var all, heavy float64
	for _, f := range fns {
		m := float64(f.cc * f.sloc)
		all += m
		if f.cc > 10 {
			heavy += m
		}
	}
	sort.Slice(fns, func(i, j int) bool { return fns[i].cc*fns[i].sloc > fns[j].cc*fns[j].sloc })
	fmt.Printf("%-40s функций %6d  пропущено %5d  erosion %.3f\n", roots[0], len(fns), skipped, heavy/all)
	for _, f := range fns[:min(6, len(fns))] {
		fmt.Printf("    cc %3d  sloc %4d  %s\n", f.cc, f.sloc, f.name)
	}
}

// sloc - значащие строки: без пустых и без строк, где только комментарий.
func sloc(b []byte) int {
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") ||
			strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "#") {
			continue
		}
		n++
	}
	return n
}
