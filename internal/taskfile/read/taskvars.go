package read

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/leiyangyou/task/v2/internal/taskfile"

	"gopkg.in/yaml.v2"
)

// Taskvars reads a Taskvars for a given directory
func Taskvars(dir string) (taskfile.Vars, error) {
	vars := make(taskfile.Vars)

	path := filepath.Join(dir, "Taskvars.yml")
	if _, err := os.Stat(path); err == nil {
		vars, err = readTaskvars(path)
		if err != nil {
			return nil, err
		}
	}

	path = filepath.Join(dir, fmt.Sprintf("Taskvars_%s.yml", runtime.GOOS))
	if _, err := os.Stat(path); err == nil {
		osVars, err := readTaskvars(path)
		if err != nil {
			return nil, err
		}

		if vars == nil {
			vars = osVars
		} else {
			for k, v := range osVars {
				vars[k] = v
			}
		}
	}

	return vars, nil
}

func readTaskvars(file string) (taskfile.Vars, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	var vars taskfile.Vars

	err = yaml.NewDecoder(f).Decode(&vars)

	if err != nil {
		err = fmt.Errorf("unable to deserialize file %v due to: %v", file, err)
	}

	return vars, err
}
