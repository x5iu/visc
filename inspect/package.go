package inspect

import (
	"bytes"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func GetPackagePath(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		pwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(pwd, dir)
	}
	gomod, err := getGoModPath(dir)
	if err != nil {
		return getPackagePathFromGOPATH(dir)
	}
	return getPackagePathFromGoMod(dir, gomod)
}

func getGoModPath(dir string) (string, error) {
	command := exec.Command("go", "env", "GOMOD")
	command.Dir = dir
	gomod, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(gomod)), nil
}

func getPackagePathFromGOPATH(dir string) (string, error) {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}
	for _, p := range strings.Split(gopath, string(filepath.ListSeparator)) {
		basepath := filepath.Join(p, "src") + string(filepath.Separator)
		rel, err := filepath.Rel(basepath, dir)
		if err == nil && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return path.Clean(filepath.ToSlash(rel)), nil
		}
	}
	return "", fmt.Errorf("can't find dir %q in GOPATH %q", dir, gopath)
}

func getPackagePathFromGoMod(dir string, gomod string) (string, error) {
	module, err := getModulePath(gomod)
	if err != nil {
		return "", err
	}
	return path.Clean(
		path.Join(
			module,
			filepath.ToSlash(
				strings.TrimPrefix(dir, filepath.Dir(gomod)),
			),
		),
	), nil
}

func getModulePath(gomod string) (string, error) {
	content, err := os.ReadFile(gomod)
	if err != nil {
		return "", err
	}
	return modulePath(content), nil
}

// Content of this file was copied from the package golang.org/x/mod/modfile
// https://github.com/golang/mod/blob/v0.2.0/modfile/read.go#L877
var (
	slashSlash = []byte("//")
	moduleStr  = []byte("module")
)

// modulePath returns the module path from the gomod file text.
// If it cannot find a module path, it returns an empty string.
// It is tolerant of unrelated problems in the go.mod file.
func modulePath(mod []byte) string {
	for len(mod) > 0 {
		line := mod
		mod = nil
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, mod = line[:i], line[i+1:]
		}
		if i := bytes.Index(line, slashSlash); i >= 0 {
			line = line[:i]
		}
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, moduleStr) {
			continue
		}
		line = line[len(moduleStr):]
		n := len(line)
		line = bytes.TrimSpace(line)
		if len(line) == n || len(line) == 0 {
			continue
		}

		if line[0] == '"' || line[0] == '`' {
			p, err := strconv.Unquote(string(line))
			if err != nil {
				return "" // malformed quoted string or multiline module path
			}
			return p
		}

		return string(line)
	}
	return "" // missing module path
}
