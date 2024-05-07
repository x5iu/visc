package cmd

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"github.com/x5iu/visc/inspect"
	goimport "golang.org/x/tools/imports"

	_ "embed"
)

var (
	ProgramName     = "visc"
	GeneratorName   = "visc"
	DirectivePrefix = "visc:"
)

const Version = "v0.6.2"

//go:embed gen.tmpl
var genTemplate string

const (
	EnvGoFile = "GOFILE"
)

var (
	buildTags   string
	output      string
	targetTypes []string

	setter            bool
	setPrefix         string
	getter            bool
	getPrefix         string
	makeConstructor   bool
	constructorName   string
	constructorPrefix string
)

var Command = &cobra.Command{
	Use:     ProgramName,
	Version: Version,
	PreRun: func(cmd *cobra.Command, args []string) {
		if len(targetTypes) != 1 && (setter || getter || makeConstructor) {
			log.Fatalln("If you need to generate setter/getter/constructor through command-line arguments, " +
				"please specify one and only one unique type.")
		}
	},
	Run: func(cmd *cobra.Command, args []string) {
		var dir string
		file := os.Getenv(EnvGoFile)
		if file == "" {
			dir, _ = os.Getwd()
		} else {
			dir = filepath.Dir(file)
		}

		pkg, err := inspect.Scan(dir, args, targetTypes)
		if err != nil {
			log.Fatalln(err)
		}

		g := &Generator{
			Package:    pkg,
			Generator:  GeneratorName,
			Tag:        buildTags,
			mustImport: make(map[*inspect.Import]struct{}),
		}
		g.preload()

		var code bytes.Buffer
		if err = template.Must(
			template.New(ProgramName).Parse(genTemplate),
		).Execute(&code, g); err != nil {
			log.Fatalln(err)
		}

		formatted, err := format.Source(code.Bytes())
		if err != nil {
			log.Fatalln(err)
		}

		if err = os.WriteFile(output, formatted, 0644); err != nil {
			log.Fatalln(err)
		}

		fixed, err := goimport.Process(output, formatted, nil)
		if err != nil {
			log.Fatalln(err)
		}

		if err = os.WriteFile(output, fixed, 0644); err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	log.SetPrefix(ProgramName + ": ")
	log.SetFlags(log.Lmsgprefix)

	flags := Command.PersistentFlags()
	flags.StringVar(&buildTags, "buildtags", "", "tags attached to output file")
	flags.StringVar(&output, "output", "", "output file")
	flags.StringSliceVar(&targetTypes, "types", nil, "target type")
	Command.MarkPersistentFlagRequired("output")

	flags.BoolVar(&setter, "setter", false, "setter flag")
	flags.StringVar(&setPrefix, "setPrefix", "", "setter prefix")
	flags.BoolVar(&getter, "getter", false, "getter flag")
	flags.StringVar(&getPrefix, "getPrefix", "", "getter prefix")
	flags.BoolVar(&makeConstructor, "construct", false, "construct flag")
	flags.StringVar(&constructorName, "constructor", "", "construct name")
	flags.StringVar(&constructorPrefix, "constructPrefix", "", "construct prefix")
}

type Generator struct {
	*inspect.Package
	Generator  string
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
	sort.SliceStable(g.Targets, func(i, j int) bool {
		return g.Targets[i].String() < g.Targets[j].String()
	})
	for _, target := range g.Targets {
		g.genGetterSetter(target)
	}
}

var fnRe = regexp.MustCompile(`^(\w+?)\((.+?)\)$`)

func (g *Generator) genGetterSetter(t *inspect.Type) {
	fmt.Fprintf(&g.out, "\n\n")
	var (
		all          bool
		allGetPrefix string
		allSetPrefix string
		allGetter    bool
		allSetter    bool
	)
	var (
		construct       bool
		constructName   string
		constructPrefix string
	)
	if t.Decl.Doc != nil || t.Spec.Doc != nil {
		list := make([]*ast.Comment, 8)
		if t.Decl.Doc != nil {
			list = append(list, t.Decl.Doc.List...)
		}
		if t.Spec.Doc != nil {
			list = append(list, t.Spec.Doc.List...)
		}
		drtAll := getDirective(list, "all")
		if all = drtAll != ""; all {
			allGetPrefix, _ = drtAll.Lookup("getPrefix")
			allSetPrefix, _ = drtAll.Lookup("setPrefix")
			allGetterOpt, found := drtAll.Lookup("getter")
			if b, err := strconv.ParseBool(allGetterOpt); found && err == nil {
				allGetter = b
			}
			allSetterOpt, found := drtAll.Lookup("setter")
			if b, err := strconv.ParseBool(allSetterOpt); found && err == nil {
				allSetter = b
			}
		}
		drtConstruct := getDirective(list, "construct")
		if construct = drtConstruct != ""; construct {
			var found bool
			constructPrefix, _ = drtConstruct.Lookup("prefix")
			constructName, found = drtConstruct.Lookup("name")
			if !found {
				constructName = "construct"
			}
		}
	}
	if !all {
		all = getter || setter
	}
	if allGetPrefix == "" {
		allGetPrefix = getPrefix
	}
	if allSetPrefix == "" {
		allSetPrefix = setPrefix
	}
	if !allGetter {
		allGetter = getter
	}
	if !allSetter {
		allSetter = setter
	}
	if !construct {
		construct = makeConstructor || constructorName != ""
	}
	if constructName == "" {
		constructName = constructorName
	}
	if constructPrefix == "" {
		constructPrefix = constructorPrefix
	}
	receiver := t.String()
	structType := t.Spec.Type.(*ast.StructType)
	cx := make([]*constructCtx, 0, len(structType.Fields.List))
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			var tag reflect.StructTag
			if lit := field.Tag; lit != nil {
				tag = reflect.StructTag(lit.Value[1 : len(lit.Value)-1])
			}
			getter, hasGetter, isRef, setter, hasSetter := inspectField(
				name.String(),
				tag,
			)
			if getterTag := tag.Get("getter"); getterTag != "-" && all && allGetter && !hasGetter {
				getter, hasGetter, isRef = allGetPrefix+toCamel(name.String()), true, false
			}
			if setterTag := tag.Get("setter"); setterTag != "-" && all && allSetter && !hasSetter {
				setter, hasSetter = allSetPrefix+toCamel(name.String()), true
			}
			var typ string
			if hasGetter || hasSetter {
				typ = g.toString(field.Type)
			}
			if hasGetter {
				genFieldGetter(&g.out, receiver, getter, name.String(), typ, isRef)
			}
			if constructFunc, ok := tag.Lookup("construct"); ok {
				if match := fnRe.FindStringSubmatch(constructFunc); match != nil && len(match) > 2 {
					cx = append(cx, &constructCtx{
						Field: name.String(),
						Type:  match[2],
						Set:   match[1],
					})
				}
			} else if hasSetter {
				genFieldSetter(&g.out, receiver, setter, name.String(), typ)
				cx = append(cx, &constructCtx{
					Field: name.String(),
					Type:  typ,
					Set:   setter,
				})
			}
		}
	}
	if construct {
		g.genConstruct(receiver, constructName, constructPrefix, cx)
	}
}

func (g *Generator) toString(expr ast.Expr) string {
	var buf strings.Builder
	if err := format.Node(&buf, g.GetFset(), expr); err != nil {
		log.Fatalln(err)
	}
	//ast.Inspect(expr, func(node ast.Node) bool {
	//	switch x := node.(type) {
	//	case ast.Expr:
	//		if named, ok := g.GetInfo().TypeOf(x).(*types.Named); ok {
	//			pkg := named.Obj().Pkg()
	//			for _, rawImport := range g.Imports {
	//				if rawImport.Path == pkg.Path() {
	//					g.mustImport[rawImport] = struct{}{}
	//				}
	//			}
	//		}
	//	}
	//	return true
	//})
	return buf.String()
}

type constructCtx struct {
	Field string
	Type  string
	Set   string
}

func (g *Generator) genConstruct(receiver string, name string, prefix string, cx []*constructCtx) {
	fmt.Fprintf(&g.out, "\n\nfunc (instance *%s) %s(constructor interface { \n", receiver, name)
	for _, getter := range cx {
		fmt.Fprintf(&g.out, "%s%s() %s\n", prefix, toCamel(getter.Field), getter.Type)
	}
	fmt.Fprintf(&g.out, "}) *%s { \n", receiver)
	for _, setter := range cx {
		fmt.Fprintf(&g.out, "instance.%s(constructor.%s%s())\n", setter.Set, prefix, toCamel(setter.Field))
	}
	fmt.Fprintf(&g.out, "return instance\n")
	fmt.Fprintf(&g.out, "}")
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
		if strings.HasPrefix(text, DirectivePrefix) {
			text = text[len(DirectivePrefix):]
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
