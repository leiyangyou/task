package templater

import (
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
)

var (
	templateFuncs template.FuncMap
)

func init() {
	templateFuncs = sprig.TxtFuncMap()

	sprigEmpty := templateFuncs["empty"].(func(interface{}) bool)
	sprigCat := templateFuncs["cat"].(func(...interface{}) string)

	empty := func(given interface {}) bool {
		return sprigEmpty(given) || given == "<no value>"
	}

	compact := func (given ...interface{}) (list []interface{}) {
		for _, value := range given {
			if !empty(value) {
				list = append(list, value)
			}
		}
		return  list
	}

	taskFuncs := template.FuncMap{
		"OS":   func() string { return runtime.GOOS },
		"ARCH": func() string { return runtime.GOARCH },
		"catLines": func(s string) string {
			s = strings.Replace(s, "\r\n", " ", -1)
			return strings.Replace(s, "\n", " ", -1)
		},
		"splitLines": func(s string) []string {
			s = strings.Replace(s, "\r\n", "\n", -1)
			return strings.Split(s, "\n")
		},
		"fromSlash": func(path string) string {
			return filepath.FromSlash(path)
		},
		"toSlash": func(path string) string {
			return filepath.ToSlash(path)
		},
		"exeExt": func() string {
			if runtime.GOOS == "windows" {
				return ".exe"
			}
			return ""
		},
		"default": func (d interface{}, given ...interface{}) interface{} {
			if empty(given) || empty(given[0]) {
				return d
			}
			return given[0]
		},
		"empty": empty,
		"compact": compact,
		"ccat": func (given ...interface{}) string {
			for i, value := range given {
				v, isString := value.(string)
				if isString {
					given[i] = strings.TrimSpace(v)
				}
			}
			given = compact(given...)

			return sprigCat(given...)
		},
		// IsSH is deprecated.
		"IsSH": func() bool { return true },
	}
	// Deprecated aliases for renamed functions.
	taskFuncs["FromSlash"] = taskFuncs["fromSlash"]
	taskFuncs["ToSlash"] = taskFuncs["toSlash"]
	taskFuncs["ExeExt"] = taskFuncs["exeExt"]

	for k, v := range taskFuncs {
		templateFuncs[k] = v
	}
}
