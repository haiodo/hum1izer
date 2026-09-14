// Прототип: цикломатика через tree-sitter, один обход на все языки.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsc "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsjava "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tscs "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tspy "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tsrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tsts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func langOf(path string) *ts.Language {
	switch filepath.Ext(path) {
	case ".go":
		return ts.NewLanguage(tsgo.Language())
	case ".c", ".h":
		return ts.NewLanguage(tsc.Language())
	case ".java":
		return ts.NewLanguage(tsjava.Language())
	case ".py":
		return ts.NewLanguage(tspy.Language())
	case ".rs":
		return ts.NewLanguage(tsrust.Language())
	case ".cs":
		return ts.NewLanguage(tscs.Language())
	case ".ts":
		return ts.NewLanguage(tsts.LanguageTypescript())
	case ".tsx":
		return ts.NewLanguage(tsts.LanguageTSX())
	}
	return nil
}

// dump: гистограмма имён узлов, чтобы не гадать про имена в грамматиках.
func dump(path string) {
	lang := langOf(path)
	src, _ := os.ReadFile(path)
	p := ts.NewParser()
	p.SetLanguage(lang)
	tree := p.Parse(src, nil)
	kinds := map[string]int{}
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		kinds[n.Kind()]++
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	type kv struct {
		k string
		n int
	}
	var all []kv
	for k, n := range kinds {
		if strings.ContainsAny(k, "(){}[];,.=+-*/<>!&|\"'") || k == "" {
			continue
		}
		all = append(all, kv{k, n})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].n > all[j].n })
	fmt.Println("==", path)
	for _, e := range all {
		if strings.Contains(e.k, "statement") || strings.Contains(e.k, "clause") ||
			strings.Contains(e.k, "function") || strings.Contains(e.k, "method") ||
			strings.Contains(e.k, "expression") || strings.Contains(e.k, "declaration") ||
			strings.Contains(e.k, "case") || strings.Contains(e.k, "when") ||
			strings.Contains(e.k, "lambda") || strings.Contains(e.k, "catch") {
			fmt.Printf("  %5d  %s\n", e.n, e.k)
		}
	}
}

func main() {
	if os.Getenv("DUMP") != "" {
		for _, a := range os.Args[1:] {
			dump(a)
		}
		return
	}
	for _, a := range os.Args[1:] {
		if os.Getenv("STATS") != "" {
			runStats(filepath.Base(a), a)
			continue
		}
		if os.Getenv("ERR") != "" {
			errStats(a)
			continue
		}
		runCC([]string{a})
	}
}
