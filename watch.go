package task

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/leiyangyou/task/v2/internal/taskfile"
	"github.com/leiyangyou/task/v2/internal/status"
	"github.com/fsnotify/fsnotify"
)


type void struct{}

const rescanTime = time.Second
const debounceTime = 500 * time.Millisecond

func (e *Executor) runCalls(calls ...taskfile.Call) (ctx context.Context, cancel context.CancelFunc) {
	ctx, cancel = context.WithCancel(context.Background())
	for _, c := range calls {
		c := c
		go func() {
			if err := e.RunTask(ctx, c); err != nil && !isContextError(err) {
				e.Logger.Errf("%v", err)
			}
		}()
	}
	return
}

func (e *Executor) isIgnored(file string) bool {
	return strings.Contains(file, "/.git/") ||
		strings.Contains(file, "/node_modules/") ||
		strings.Contains(file, "/.")

}

// watchTasks start watching the given tasks
func (e *Executor) watchTasks(calls ...taskfile.Call) error {
	interrupted := make(chan void)
	wg := sync.WaitGroup{}
	wg.Add(2)

	tasks := make([]string, len(calls))
	for i, c := range calls {
		tasks[i] = c.Task
	}

	ctx, cancel := e.runCalls(calls...)

	w, err := fsnotify.NewWatcher()
	if err != nil {
		e.Logger.Errf("task: Unable to start watcher")
		return err
	} else {
		e.Logger.Errf("task: Started watching for tasks: %s", strings.Join(tasks, ", "))
	}

	defer func(){
		_ = w.Close()
	}()

	closeOnInterrupt(interrupted)

	debounce, cancelDebounce := newDebouncer(debounceTime)

	go func() {
		for {
			select {
			case event := <-w.Events:
				if event.Op != fsnotify.Chmod {
					e.Logger.VerboseErrf("task: received watch event: %v", event)
					debounce(func () {
						e.Logger.VerboseErrf("task: triggering rerun: %v", event)
						cancel()
						ctx, cancel = e.runCalls(calls...)
					})
				}
			case err := <-w.Errors:
				e.Logger.Errf("task: watcher error: %v", err)
			case <-interrupted:
				cancel()
				cancelDebounce()
				wg.Done()
				return
			}
		}
	}()

	go func() {
		watchedFiles := make(map[string]void)

		// re-register each second because we can have new files
		for {
			if err := e.registerWatchedFiles(w, watchedFiles, calls...); err != nil {
				e.Logger.Errf("task: file registration error: %v", err)
			}
			select {
				case <-interrupted:
					wg.Done()
				default:
			}
			time.Sleep(rescanTime)
		}
	}()

	wg.Wait()

	return nil
}

type debouncer struct {
	mu sync.Mutex
	after time.Duration
	timer *time.Timer
}

func newDebouncer(after time.Duration) (func(f func()), func()) {
	d := &debouncer{after: after}

	return func(f func()) {
		d.add(f)
	}, func() {
		d.cancel()
	}
}

func (d *debouncer) add(f func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reallyCancel()
	d.timer = time.AfterFunc(d.after, f)
}

func (d *debouncer) reallyCancel() {
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}
func (d *debouncer) cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reallyCancel()

}

func isContextError(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}

func closeOnInterrupt(interrupted chan void) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func() {
		<-ch
		close(interrupted)
	}()
}



func (e *Executor) registerWatchedFiles(w *fsnotify.Watcher, watchedFiles map[string]void, calls ...taskfile.Call) error {
	member := void{}

	newWatchedFiles := make(map[string]void)
	generatedFiles := make([]string, 0)

	var registerTaskFiles func(taskfile.Call) error
	registerTaskFiles = func(c taskfile.Call) error {
		task, err := e.CompiledTask(c)
		if err != nil {
			return err
		}

		for _, d := range task.Deps {
			if err := registerTaskFiles(taskfile.Call{Task: d.Task, Vars: d.Vars}); err != nil {
				return err
			}
		}
		for _, c := range task.Cmds {
			if c.Task != "" {
				if err := registerTaskFiles(taskfile.Call{Task: c.Task, Vars: c.Vars}); err != nil {
					return err
				}
			}
		}

		dir, err := filepath.Abs(task.Dir)
		if err != nil {
			e.Logger.Errf("unable to resolve directory %v", task.Dir)
			return err
		}

		files, err := status.Glob(dir, task.Sources)

		if err != nil {
			e.Logger.Errf("unable to glob sources in %s: %v", task.Dir, task.Sources)
			return err
		}

		generated, err := status.Glob(dir, task.Generates)

		for _, f := range generated {
			generatedFiles = append(generatedFiles, f)
		}


		if err != nil {
			e.Logger.Errf("unable to glob generates in %s: %v", task.Dir, task.Sources)
			return err
		}

		for _, f := range files {
			newWatchedFiles[f] = member
		}

		return nil
	}

	for _, c := range calls {
		if err := registerTaskFiles(c); err != nil {
			return err
		}
	}

	for _, f := range generatedFiles {
		delete(newWatchedFiles, f)
	}

	oldWatchedFiles := make(map[string]void)

	for f := range watchedFiles {
		if _, ok := newWatchedFiles[f]; !ok {
			err := w.Remove(f)
			delete(watchedFiles, f)
			if err != nil {
				return err
			} else {
				e.Logger.VerboseErrf("task: unwatching file %s", f)
			}
		} else {
			oldWatchedFiles[f] = member
		}
	}

	for f := range newWatchedFiles {
		if _, ok := oldWatchedFiles[f]; !ok && !e.isIgnored(f){
			err := w.Add(f)
			if  err != nil {
				return err
			} else {
				e.Logger.VerboseErrf("task: watching file %s", f)
				watchedFiles[f] = member
			}
		}
	}

	return nil
}
