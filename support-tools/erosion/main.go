// Прототип метрик из SlopCodeBench для Go: erosion и доля клонов.
package main

import (
	"crypto/sha1"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fn struct {
	name string
	cc   int
	sloc int
}

func cyclo(b *ast.BlockStmt) int {
	cc := 1
	ast.Inspect(b, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.CaseClause, *ast.CommClause:
			cc++
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				cc++
			}
		}
		return true
	})
	return cc
}

func main() {
	root := os.Args[1]
	fset := token.NewFileSet()
	var fns []fn
	loc, clones := 0, 0
	seen := map[string]int{}
	var win []string
	var winLines [][]int

	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if n == "vendor" || n == "build" || n == "SourcePackages" || n == "testdata" || n == ".git" || n == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			return nil
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			s := fset.Position(fd.Body.Pos()).Line
			e := fset.Position(fd.Body.End()).Line
			fns = append(fns, fn{p + ":" + fd.Name.Name, cyclo(fd.Body), e - s + 1})
		}
		// клоны: окно из 5 значащих строк кода, хэш; повтор - клон.
		win, winLines = win[:0], winLines[:0]
		var nums []int
		for i, l := range strings.Split(string(src), "\n") {
			t := strings.TrimSpace(l)
			if t == "" || strings.HasPrefix(t, "//") {
				continue
			}
			loc++
			win = append(win, t)
			nums = append(nums, i)
		}
		mark := map[int]bool{}
		for i := 0; i+5 <= len(win); i++ {
			h := fmt.Sprintf("%x", sha1.Sum([]byte(strings.Join(win[i:i+5], "\n"))))
			if seen[h] > 0 {
				for j := i; j < i+5; j++ {
					mark[nums[j]] = true
				}
			}
			seen[h]++
		}
		clones += len(mark)
		return nil
	})

	var all, heavy float64
	for _, f := range fns {
		m := float64(f.cc * f.sloc)
		all += m
		if f.cc > 10 {
			heavy += m
		}
	}
	sort.Slice(fns, func(i, j int) bool { return fns[i].cc*fns[i].sloc > fns[j].cc*fns[j].sloc })
	fmt.Printf("%-40s функций %5d  LOC %7d\n", root, len(fns), loc)
	fmt.Printf("  erosion  %.3f   (пост: репы 0.31+-0.17, агенты 0.68+-0.20)\n", heavy/all)
	fmt.Printf("  клоны    %.3f   (пост: verbosity репы 0.15+-0.06, агенты 0.33+-0.10)\n", float64(clones)/float64(loc))
	fmt.Println("  тяжелее всего:")
	for _, f := range fns[:min(5, len(fns))] {
		fmt.Printf("    cc %3d  sloc %4d  %s\n", f.cc, f.sloc, f.name)
	}
}
