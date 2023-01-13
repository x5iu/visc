package main

import (
	"flag"
	"fmt"
	"github.com/x5iu/visc/inspect"
	"log"
	"os"
	"os/exec"
	"path"
	"text/template"
	"unicode"

	_ "embed"
)

const Version = "v0.1.0"

//go:embed gen.tmpl
var genTemplate string

const (
	EnvGoFile = "GOFILE"
)

var (
	buildTags = flag.String("buildtags", "", "tags attached to output file")
	genTags   = flag.String("gentags", "", "tags when executing \"go run visc.*.go\"")
	output    = flag.String("output", "visc.gen.go", "output file")
	version   = flag.Bool("version", false, "visc version")
	keepTemp  = flag.Bool("keeptemp", false, "keep temp visc.*.go file")
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

	dir := path.Dir(file)
	pkg, err := inspect.Scan(dir, flag.Args())
	if err != nil {
		log.Fatalln(err)
	}

	if pkg.Name == "main" {
		log.Fatalln("error: visc does not work on main package")
	}

	ctx := new(Context)
	ctx.Output = *output
	ctx.Generics = make(map[string]struct{})
	ctx.Package.Tags = *buildTags
	ctx.Package.Name = pkg.Name
	ctx.Package.Path, err = inspect.GetPackagePath(dir)
	if err != nil {
		log.Fatalln(err)
	}

	for _, target := range pkg.Targets {
		if target.Spec.TypeParams != nil {
			for _, field := range target.Spec.TypeParams.List {
				for _, name := range field.Names {
					ctx.Generics[name.String()] = struct{}{}
				}
			}
		}
		targetType := target.String()
		if len(targetType) > 0 && unicode.IsUpper([]rune(targetType)[0]) {
			ctx.Types = append(ctx.Types, targetType)
		}
	}

	if err = ctx.Execute(dir); err != nil {
		log.Fatalln(err)
	}
}

type Context struct {
	Package struct {
		Name string
		Path string
		Tags string
	}
	Generics map[string]struct{}
	Types    []string
	Output   string
}

func (ctx *Context) Execute(dir string) error {
	temp, err := os.CreateTemp(dir, "visc.*.go")
	if err != nil {
		return fmt.Errorf("os.CreateTemp: %w", err)
	}

	if !*keepTemp {
		defer os.Remove(temp.Name())
	}

	defer temp.Close()

	if err = template.Must(template.New("visc").Parse(genTemplate)).
		Execute(temp, ctx); err != nil {
		return fmt.Errorf("templte.Execute: %w", err)
	}

	args := make([]string, 0, 5)
	args = append(args, "run")
	if *genTags != "" {
		args = append(args, "-tags", *genTags)
	}
	args = append(args, path.Base(temp.Name()))

	command := exec.Command("go", args...)
	command.Dir = path.Dir(temp.Name())
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err = command.Run(); err != nil {
		return fmt.Errorf("command.Run: %w", err)
	}

	return nil
}
