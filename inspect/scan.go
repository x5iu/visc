package inspect

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path"
	"strings"
)

type Package struct {
	Name    string
	Targets []*Type
}

type Type struct {
	FileSet *token.FileSet
	Spec    *ast.TypeSpec
}

func (t *Type) String() string {
	var buf bytes.Buffer
	printer.Fprint(&buf, t.FileSet, t.Spec)
	spec := buf.String()
	spec = spec[t.Spec.Name.Pos()-t.Spec.Pos() : t.Spec.Name.End()-t.Spec.Pos()]
	if t.Spec.TypeParams != nil {
		generics := make([]string, 0, len(t.Spec.TypeParams.List))
		for _, field := range t.Spec.TypeParams.List {
			for _, name := range field.Names {
				generics = append(generics, name.String())
			}
		}
		spec = spec + "[" + strings.Join(generics, ", ") + "]"
	}
	return spec
}

func Scan(dir string, files []string) (*Package, error) {
	if !path.IsAbs(dir) {
		pwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		dir = path.Join(pwd, dir)
	}
	fset := token.NewFileSet()
	packages, err := parser.ParseDir(fset, dir, filter(files), 0)
	if err != nil {
		return nil, err
	}
	if l := len(packages); l != 1 {
		return nil, fmt.Errorf("%d packages found", l)
	}
	out := &Package{
		Targets: make([]*Type, 0, 10),
	}
	for _, p := range packages {
		ast.Inspect(p, func(input ast.Node) bool {
			switch node := input.(type) {
			case *ast.Package:
				out.Name = node.Name
				return true
			case *ast.File, *ast.GenDecl:
				return true
			case *ast.TypeSpec:
				if _, ok := node.Type.(*ast.StructType); ok {
					out.Targets = append(out.Targets, &Type{
						FileSet: fset,
						Spec:    node,
					})
				}
				return false
			}
			return false
		})
	}
	return out, nil
}

func filter(files []string) func(info fs.FileInfo) bool {
	return func(info fs.FileInfo) bool {
		if len(files) == 0 {
			return true
		}
		for _, file := range files {
			if info.Name() == path.Base(file) {
				return true
			}
		}
		return false
	}
}
