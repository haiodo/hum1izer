package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// errStats: сколько файлов парсится с ошибками и сколько узлов ERROR внутри.
func errStats(root string) {
	parsers := map[string]*ts.Parser{}
	files, bad, errNodes := 0, 0, 0
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "testdata", ".git", "node_modules", "build", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		lang := langOf(p)
		if lang == nil || strings.HasSuffix(p, "_test.go") {
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
		files++
		n := 0
		var walk func(x *ts.Node)
		walk = func(x *ts.Node) {
			if x.IsError() || x.IsMissing() {
				n++
				return
			}
			for i := uint(0); i < x.ChildCount(); i++ {
				walk(x.Child(i))
			}
		}
		walk(tree.RootNode())
		if n > 0 {
			bad++
			errNodes += n
		}
		tree.Close()
		return nil
	})
	fmt.Printf("%-44s файлов %6d  с ошибкой разбора %5d (%4.1f%%)  узлов ERROR %6d\n",
		root, files, bad, float64(bad)*100/float64(max(files, 1)), errNodes)
}
