package inspect

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Package struct {
	fset *token.FileSet
	info *types.Info

	Name    string
	Imports []*Import
	Targets []*Type
}

func (p *Package) GetFset() *token.FileSet {
	return p.fset
}

func (p *Package) GetInfo() *types.Info {
	return p.info
}

type Import struct {
	Name string
	Path string
}

type Type struct {
	Fset *token.FileSet
	Decl *ast.GenDecl
	Spec *ast.TypeSpec
}

func (t *Type) String() string {
	var buf bytes.Buffer
	printer.Fprint(&buf, t.Fset, t.Spec)
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

func Scan(dir string, files []string, types []string) (*Package, error) {
	if !filepath.IsAbs(dir) {
		pwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(pwd, dir)
	}
	fset := token.NewFileSet()
	packages, err := parser.ParseDir(fset, dir, filter(files), parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if l := len(packages); l != 1 {
		return nil, fmt.Errorf("%d packages found", l)
	}
	out := &Package{
		fset:    fset,
		Imports: make([]*Import, 0, 8),
		Targets: make([]*Type, 0, 8),
	}
	for _, p := range packages {
		astFiles := make([]*ast.File, 0, len(p.Files))
		for _, file := range p.Files {
			astFiles = append(astFiles, file)
		}
		//conf := types.Config{
		//	IgnoreFuncBodies: true,
		//	FakeImportC:      true,
		//	Importer: &Importer{
		//		imported:      map[string]*types.Package{},
		//		tokenFileSet:  fset,
		//		defaultImport: importer.Default(),
		//	},
		//}
		//info := &types.Info{
		//	Types:      map[ast.Expr]types.TypeAndValue{},
		//	Instances:  map[*ast.Ident]types.Instance{},
		//	Defs:       map[*ast.Ident]types.Object{},
		//	Uses:       map[*ast.Ident]types.Object{},
		//	Implicits:  map[ast.Node]types.Object{},
		//	Selections: map[*ast.SelectorExpr]*types.Selection{},
		//	Scopes:     map[ast.Node]*types.Scope{},
		//	InitOrder:  []*types.Initializer{},
		//}
		//_, err = conf.Check(p.Name, fset, astFiles, info)
		//if err != nil {
		//	return nil, err
		//}
		//out.info = info
		//imports := make(map[string]*ast.Ident, 8)
		//imported := make(map[string]struct{})
		ast.Inspect(p, func(input ast.Node) bool {
			switch node := input.(type) {
			case *ast.Package:
				out.Name = node.Name
				return true
			//case *ast.ImportSpec:
			//	path, _ := strconv.Unquote(node.Path.Value)
			//	imports[path] = node.Name
			//	return true
			case *ast.File:
				return true
			case *ast.GenDecl:
				if node.Tok == token.TYPE {
					for _, spec := range node.Specs {
						if typeSpec, ok := spec.(*ast.TypeSpec); ok {
							if _, ok := typeSpec.Type.(*ast.StructType); ok {
								if filterTypes(types, typeSpec.Name.String()) {
									out.Targets = append(out.Targets, &Type{
										Fset: fset,
										Decl: node,
										Spec: typeSpec,
									})
									//ast.Inspect(typeSpec.Type, func(structType ast.Node) bool {
									//	switch expr := structType.(type) {
									//	case ast.Expr:
									//		if named, ok := info.TypeOf(expr).(*types.Named); ok {
									//			if objPkg := named.Obj().Pkg(); objPkg != nil {
									//				if alias, exists := imports[objPkg.Path()]; exists {
									//					if _, duplicated := imported[objPkg.Path()]; !duplicated {
									//						var name string
									//						if alias != nil {
									//							name = alias.String()
									//						}
									//						out.Imports = append(out.Imports, &Import{
									//							Name: name,
									//							Path: objPkg.Path(),
									//						})
									//						imported[objPkg.Path()] = struct{}{}
									//					}
									//				}
									//			}
									//		}
									//	}
									//	return true
									//})
								}
							}
						}
					}
				}
				return true
			}
			return false
		})
	}
	return out, nil
}

func filterTypes(types []string, defType string) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if t == defType {
			return true
		}
	}
	return false
}

func filter(files []string) func(info fs.FileInfo) bool {
	return func(info fs.FileInfo) bool {
		if len(files) == 0 {
			return true
		}
		for _, file := range files {
			if info.Name() == filepath.Base(file) {
				return true
			}
		}
		return false
	}
}

type Importer struct {
	imported      map[string]*types.Package
	tokenFileSet  *token.FileSet
	defaultImport types.Importer
}

var importing types.Package

func (importer *Importer) ImportFrom(path, dir string, _ types.ImportMode) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if path == "C" {
		return importer.defaultImport.Import("C")
	}
	goroot := filepath.Join(build.Default.GOROOT, "src")
	if _, err := os.Stat(filepath.Join(goroot, path)); err != nil {
		if os.IsNotExist(err) {
			target := importer.imported[path]
			if target != nil {
				if target == &importing {
					return nil, errors.New("cycle importing " + path)
				}
				return target, nil
			}
			importer.imported[path] = &importing
			pkg, err := build.Import(path, dir, 0)
			if err != nil {
				return nil, err
			}
			var files []*ast.File
			for _, name := range append(pkg.GoFiles, pkg.CgoFiles...) {
				name = filepath.Join(pkg.Dir, name)
				file, err := parser.ParseFile(importer.tokenFileSet, name, nil, 0)
				if err != nil {
					return nil, err
				}
				files = append(files, file)
			}
			conf := types.Config{
				Importer:         importer,
				FakeImportC:      true,
				IgnoreFuncBodies: true,
			}
			target, err = conf.Check(path, importer.tokenFileSet, files, nil)
			if err != nil {
				return nil, err
			}
			importer.imported[path] = target
			return target, nil
		}
	}
	return importer.defaultImport.Import(path)
}

func (importer *Importer) Import(path string) (*types.Package, error) {
	return importer.ImportFrom(path, "", 0)
}
