package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestAuthenticationErrorsAreEnglish(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "auth.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "authErr") || i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					value, _ := strconv.Unquote(lit.Value)
					assertEnglishError(t, name.Name, value)
					count++
				}
			}
		}
	}
	if count == 0 {
		t.Fatal("no auth error constants inspected")
	}
}
