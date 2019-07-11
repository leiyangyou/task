package status

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/leiyangyou/task/v2/internal/execext"

	"github.com/mattn/go-zglob"
)

func Glob(dir string, globs []string) ([]string, error) {
	var included []string
	var excluded = make(map[string]struct{}, 0)

	for _, glob := range globs {
		for _, globPart := range strings.Split(glob, ":") {
			if strings.Trim(globPart, " ") == "" {
				continue
			}
			var exclude = false

			if globPart[0] == '!' {
				exclude = true
				globPart = globPart[1:]
			}

			globPart, err := execext.Expand(globPart)

			if !filepath.IsAbs(globPart) {
				globPart = filepath.Join(dir, globPart)
			}

			if err != nil {
				return nil, err
			}

			files, err := zglob.Glob(globPart)

			if err != nil {
				return nil, err
			}

			if exclude {
				for _, f := range files {
					excluded[f] = struct{}{}
				}
			} else {
				included = append(included, files...)
			}
		}
	}

	var files = make([]string, 0)

	for _, f := range included {
		if _, ok := excluded[f]; !ok {
			files = append(files, f)
		}
	}

	sort.Strings(files)

	return files, nil
}
