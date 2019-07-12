package status

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/leiyangyou/task/v2/internal/execext"

	"github.com/bmatcuk/doublestar"
)

func VisitGlobs(dir string, globs []string, visit func(glob string, exclude bool) error) error {
	for _, glob := range globs {
		for _, subGlob := range strings.Split(glob, ":") {
			if strings.Trim(subGlob, " ") == "" {
				continue
			}

			var exclude = false
			if subGlob[0] == '!' {
				exclude = true
				subGlob = subGlob[1:]
			}

			if !filepath.IsAbs(subGlob) {
				subGlob = filepath.Join(dir, subGlob)
			}

			subGlob, err := execext.Expand(subGlob)
			if err != nil {
				return err
			}

			err = visit(subGlob, exclude)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func Glob(dir string, globs []string) ([]string, error) {
	var included []string
	var excluded = make(map[string]struct{}, 0)

	err := VisitGlobs(dir, globs, func(glob string, exclude bool) error {
		files, err := doublestar.Glob(glob)

		if err != nil {
			return err
		}

		if exclude {
			for _, f := range files {
				excluded[f] = struct{}{}
			}
		} else {
			included = append(included, files...)
		}

		return nil
	})

	if err != nil {
		return nil, err
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
