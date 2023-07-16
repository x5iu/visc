package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/x5iu/visc/inspect"
	"go/ast"
	"go/format"
	"go/types"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"text/template"

	_ "embed"
)

const Version = "v0.2.0"

//go:embed gen.tmpl
var genTemplate string

const (
	EnvGoFile = "GOFILE"
)

var (
	buildTags = flag.String("buildtags", "", "tags attached to output file")
	output    = flag.String("output", "visc.gen.go", "output file")
	version   = flag.Bool("version", false, "visc version")
)

func init() {
	log.SetPrefix("visc: ")
	log.SetFlags(log.Lmsgprefix)
}

func main() {
	flag.Parse()

	if *version {
		fmt.Println("visc", Version)
		return
	}

	file := os.Getenv(EnvGoFile)
	if file == "" {
		log.Fatalln("error: visc should work with //go:generate command")
	}

	dir := filepath.Dir(file)
	pkg, err := inspect.Scan(dir, flag.Args())
	if err != nil {
		log.Fatalln(err)
	}

	g := &Generator{
		Package:    pkg,
		Tag:        *buildTags,
		mustImport: make(map[*inspect.Import]struct{}),
	}
	g.preload()

	var code bytes.Buffer
	if err = template.Must(
		template.New("visc").
			Parse(genTemplate),
	).Execute(&code, g); err != nil {
		log.Fatalln(err)
	}

	formatted, err := format.Source(code.Bytes())
	if err != nil {
		log.Fatalln(err)
	}

	if err = os.WriteFile(*output, formatted, 0644); err != nil {
		log.Fatalln(err)
	}
}

type Generator struct {
	*inspect.Package
	Tag        string
	mustImport map[*inspect.Import]struct{}
	out        strings.Builder
}

func (g *Generator) MustImport() []*inspect.Import {
	imports := make([]*inspect.Import, 0, len(g.mustImport))
	for imported := range g.mustImport {
		imports = append(imports, imported)
	}
	return imports
}

func (g *Generator) Code() string {
	return g.out.String()
}

func (g *Generator) preload() {
	for _, target := range g.Targets {
		g.genGetterSetter(target)
	}
}

func (g *Generator) genGetterSetter(t *inspect.Type) {
	var (
		all       bool
		allPrefix string
		allGetter bool
		allSetter bool
	)
	if t.Decl.Doc != nil || t.Spec.Doc != nil {
		var found bool
		list := make([]*ast.Comment, 8)
		if t.Decl.Doc != nil {
			list = append(list, t.Decl.Doc.List...)
		}
		if t.Spec.Doc != nil {
			list = append(list, t.Spec.Doc.List...)
		}
		drtAll := getDirective(list, "all")
		allPrefix, found = drtAll.Lookup("prefix")
		all = found && drtAll != ""
		if all {
			allGetterOpt, found := drtAll.Lookup("getter")
			if b, err := strconv.ParseBool(allGetterOpt); found && err == nil {
				allGetter = b
			}
			allSetterOpt, found := drtAll.Lookup("setter")
			if b, err := strconv.ParseBool(allSetterOpt); found && err == nil {
				allSetter = b
			}
		}
	}
	receiver := t.String()
	for _, field := range t.Spec.Type.(*ast.StructType).Fields.List {
		for _, name := range field.Names {
			var tag reflect.StructTag
			if lit := field.Tag; lit != nil {
				tag = reflect.StructTag(lit.Value[1 : len(lit.Value)-1])
			}
			getter, hasGetter, isRef, setter, hasSetter := inspectField(
				name.String(),
				tag,
			)
			if getterTag := tag.Get("getter"); getterTag != "-" && all && allGetter {
				getter, hasGetter, isRef = allPrefix+toCamel(name.String()), true, false
			}
			if setterTag := tag.Get("setter"); setterTag != "-" && all && allSetter {
				setter, hasSetter = "Set"+toCamel(name.String()), true
			}
			var typ string
			if hasGetter || hasSetter {
				typ = g.toString(field.Type)
			}
			if hasGetter {
				genFieldGetter(&g.out, receiver, getter, name.String(), typ, isRef)
			}
			if hasSetter {
				genFieldSetter(&g.out, receiver, setter, name.String(), typ)
			}
		}
	}
}

func (g *Generator) toString(expr ast.Expr) string {
	var buf strings.Builder
	if err := format.Node(&buf, g.GetFset(), expr); err != nil {
		log.Fatalln(err)
	}
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if named, ok := g.GetInfo().TypeOf(expr).(*types.Named); ok {
		pkg := named.Obj().Pkg()
		for _, rawImport := range g.Imports {
			if rawImport.Path == pkg.Path() {
				g.mustImport[rawImport] = struct{}{}
			}
		}
	}
	return buf.String()
}

func inspectField(name string, tag reflect.StructTag) (getter string, hasGetter bool, isRef bool, setter string, hasSetter bool) {
	// inspect getter
	if getterTag, ok := tag.Lookup("getter"); ok {
		getterParts := strings.Split(getterTag, ",")
		if firstPart := strings.TrimSpace(getterParts[0]); firstPart != "" && firstPart != "-" {
			hasGetter = true
			if firstPart == "*" {
				getter = toCamel(name)
			} else {
				getter = firstPart
			}
			if len(getterParts) > 1 {
				for _, part := range getterParts[1:] {
					switch strings.TrimSpace(part) {
					case "ptr", "pointer", "ref", "reference":
						isRef = true
					}
				}
			}
		}
	}

	// inspect setter
	if setterTag, ok := tag.Lookup("setter"); ok {
		setterParts := strings.Split(setterTag, ",")
		if firstPart := strings.TrimSpace(setterParts[0]); firstPart != "" && firstPart != "-" {
			hasSetter = true
			if firstPart == "*" {
				setter = "Set" + toCamel(name)
			} else {
				setter = firstPart
			}
		}
	}

	return
}

// Converts a string to CamelCase
func toCamel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	n := strings.Builder{}
	n.Grow(len(s))
	capNext := true
	for i, v := range []byte(s) {
		vIsCap := v >= 'A' && v <= 'Z'
		vIsLow := v >= 'a' && v <= 'z'
		if capNext {
			if vIsLow {
				v += 'A'
				v -= 'a'
			}
		} else if i == 0 {
			if vIsCap {
				v += 'a'
				v -= 'A'
			}
		}
		if vIsCap || vIsLow {
			n.WriteByte(v)
			capNext = false
		} else if vIsNum := v >= '0' && v <= '9'; vIsNum {
			n.WriteByte(v)
			capNext = true
		} else {
			capNext = v == '_' || v == ' ' || v == '-' || v == '.'
		}
	}
	return n.String()
}

func genFieldGetter(w io.Writer, receiver string, method string, field string, typ string, isRef bool) {
	fmt.Fprintf(w, "func (instance *%s) %s() %s%s { return %sinstance.%s }\n",
		receiver, method, refType(isRef), typ, refValue(isRef), field)
}

func refType(isRef bool) string {
	if isRef {
		return "*"
	} else {
		return ""
	}
}

func refValue(isRef bool) string {
	if isRef {
		return "&"
	} else {
		return ""
	}
}

func genFieldSetter(w io.Writer, receiver string, method string, field string, typ string) {
	fmt.Fprintf(w, "func (instance *%s) %s(value %s) { instance.%s = value }\n",
		receiver, method, typ, field)
}

const VISCPrefix = "visc:"

type Directive string

func (d Directive) Lookup(name string) (value string, found bool) {
	var (
		directive = string(d)
		param     string
	)
iter:
	for len(directive) > 0 {
		for i, c := range directive {
			switch c {
			case '=':
				param = strings.TrimSpace(directive[:i])
				directive = directive[i+1:]
				continue iter
			case ',':
				value = strings.TrimSpace(directive[:i])
				if param == name {
					return value, true
				}
				directive = directive[i+1:]
				continue iter
			}
		}
		if param == name {
			return strings.TrimSpace(directive), true
		}
		directive = ""
	}
	return "", false
}

func getDirective(list []*ast.Comment, command string) Directive {
	for _, comment := range list {
		if comment == nil {
			continue
		}
		var text string
		switch comment.Text[1] {
		case '/':
			text = strings.TrimSpace(comment.Text[2:])
		case '*':
			text = comment.Text[2 : len(comment.Text)-2]
		}
		if strings.HasPrefix(text, VISCPrefix) {
			text = text[len(VISCPrefix):]
			if text[len(text)-1] == ')' {
				for i, c := range text {
					if c == '(' {
						directive := text[:i]
						if directive == command {
							return Directive(text[i+1 : len(text)-1])
						}
					}
				}
			}
		}
	}
	return ""
}
