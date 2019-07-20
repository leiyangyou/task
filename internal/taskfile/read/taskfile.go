package read

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/leiyangyou/task/v2/internal/taskfile"

	"gopkg.in/yaml.v2"
)

const NamespaceSeparator = ":"

// Taskfile reads a Taskfile for a given directory
func Taskfile(path string, parentVars taskfile.Vars, namespaces ...string) (*taskfile.Taskfile, error) {
	dir := filepath.Dir(path)

	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf(`task: task file %s is not found, use "task --init" to create a new one`, path)
	}
	t, err := readTaskfile(path)
	if err != nil {
		return nil, err
	}

	t.Vars = parentVars.Merge(t.Vars)

	var taskNames []string

	for name := range t.Tasks {
		taskNames = append(taskNames, name)
	}

	for _, name := range taskNames {
		task := t.Tasks[name]

		nameWithNamespace := taskNameWithNamespace(name, namespaces...)

		if nameWithNamespace != name {
			delete(t.Tasks, name)
			t.Tasks[nameWithNamespace] = task
		}

		for _, dep := range task.Deps {
			dep.Task = taskNameWithNamespace(dep.Task, namespaces...)
		}

		for _, cmd := range task.Cmds {
			if cmd.Task != "" {
				cmd.Task = taskNameWithNamespace(cmd.Task, namespaces...)
			}
		}

		task.TaskfileVars = t.Vars

		task.Task = nameWithNamespace
	}

	for includedNamespace, includedPath := range t.Includes {
		includedPath = filepath.Join(dir, includedPath)
		info, err := os.Stat(includedPath)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			includedPath = filepath.Join(includedPath, "Taskfile.yml")
		}

		var includedNamespaces []string

		if strings.HasPrefix(includedNamespace, ".") {
			includedNamespaces = namespaces
		} else {
			includedNamespaces = append(namespaces, includedNamespace)
		}

		includedTaskfile, err := Taskfile(includedPath, t.Vars, includedNamespaces...)
		if err != nil {
			return nil, err
		}
		if err = taskfile.Merge(t, includedTaskfile); err != nil {
			return nil, err
		}
	}

	baseTaskName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	path = filepath.Join(dir, fmt.Sprintf("%s_%s.yml", baseTaskName, runtime.GOOS))
	if _, err = os.Stat(path); err == nil {
		osTaskfile, err := Taskfile(path, t.Vars, namespaces...)
		if err != nil {
			return nil, err
		}
		if err = taskfile.Merge(t, osTaskfile); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func readTaskfile(file string) (*taskfile.Taskfile, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	var t taskfile.Taskfile
	return &t, yaml.NewDecoder(f).Decode(&t)
}

func taskNameWithNamespace(taskName string, namespaces ...string) string {
	if strings.HasPrefix(taskName, NamespaceSeparator) {
		return strings.TrimPrefix(taskName, NamespaceSeparator)
	}

	if len(namespaces) > 0 {
		if taskName == namespaces[len(namespaces) - 1] {
			namespaces = namespaces[:len(namespaces) -1]
		}
	}

	return strings.Join(append(namespaces, taskName), NamespaceSeparator)
}
