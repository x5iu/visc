package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/x5iu/visc/inspect"
	"go/ast"
	"go/format"
	"go/token"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
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

	var code bytes.Buffer
	if err = template.Must(
		template.New("visc").
			Funcs(template.FuncMap{
				"genGetterSetter": genGetterSetter,
			}).
			Parse(genTemplate),
	).Execute(&code, &TaggedPackage{pkg, *buildTags}); err != nil {
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

type TaggedPackage struct {
	*inspect.Package
	Tag string
}

func genGetterSetter(t *inspect.Type) string {
	var w strings.Builder
	receiver := t.String()
	for _, field := range t.Spec.Type.(*ast.StructType).Fields.List {
		for _, name := range field.Names {
			var tag string
			if lit := field.Tag; lit != nil {
				tag = lit.Value[1 : len(lit.Value)-1]
			}
			getter, hasGetter, isRef, setter, hasSetter := inspectField(
				name.String(),
				reflect.StructTag(tag),
			)
			if hasGetter {
				genFieldGetter(&w, receiver, getter, name.String(), toString(t.Fset, field.Type), isRef)
			}
			if hasSetter {
				genFieldSetter(&w, receiver, setter, name.String(), toString(t.Fset, field.Type))
			}
		}
	}
	return w.String()
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

func toString(fset *token.FileSet, node ast.Node) string {
	var buf strings.Builder
	if err := format.Node(&buf, fset, node); err != nil {
		log.Fatalln(err)
	}
	return buf.String()
}
