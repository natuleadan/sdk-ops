package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

func runLint(root string) error {
	if err := runExec(root, "go", "vet", "./..."); err != nil {
		return err
	}
	fset := token.NewFileSet()
	return filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		var lintErr error
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "panic" {
				lintErr = fmt.Errorf("forbidden panic() in %s", path)
				return false
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok {
					if (pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Print")) ||
						(pkg.Name == "os" && sel.Sel.Name == "Exit") {
						lintErr = fmt.Errorf("forbidden %s.%s in %s", pkg.Name, sel.Sel.Name, path)
						return false
					}
				}
			}
			return true
		})
		return lintErr
	})
}
