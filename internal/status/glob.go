package status

import (
	"path/filepath"
	"sort"

	"github.com/go-task/task/v2/internal/execext"

	"github.com/mattn/go-zglob"
)

func glob(dir string, globs []string) (files []string, err error) {
	for _, g := range globs {
		if !filepath.IsAbs(g) {
			g = filepath.Join(dir, g)
		}
		g, err = execext.Expand(g)
		if err != nil {
			return nil, err
		}

		var exclude = false

		if g[0] == '!' {
			exclude = true
			g = g[1:]
		}
		f, err := zglob.Glob(g)
		if err != nil {
			continue
		}
		if exclude {
			var temp = make([]string, 0)
			for _, x := range files {
				var found = false

				for _, y := range f {
					if x == y {
						found = true
						break
					}
				}

				if !found {
					temp = append(temp, x)
				}
			}
			files = temp

		} else {
			files = append(files, f...)
		}
	}
	sort.Strings(files)
	return
}
