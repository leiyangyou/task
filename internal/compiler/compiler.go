package compiler

import (
	"github.com/leiyangyou/task/v2/internal/taskfile"
)

// Compiler handles compilation of a task before its execution.
// E.g. variable merger, template processing, etc.
type Compiler interface {
	GetVariables(t *taskfile.Task, call taskfile.Call) (taskfile.Vars, error)
	HandleDynamicVar(v taskfile.Var) (string, error)
	Reset()
}
