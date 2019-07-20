package task

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/leiyangyou/task/v2/internal/status"
	"github.com/leiyangyou/task/v2/internal/taskfile"
	"github.com/rjeczalik/notify"
)

type void struct{}

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
	return strings.Contains(file, "/.git/") || strings.Contains(file, "/node_modules/")
}

func (e *Executor) walkTask(call taskfile.Call, visit func(*taskfile.Task) error) error {
	task, err := e.CompiledTask(call)

	if err != nil {
		return err
	}

	for _, d := range task.Deps {
		err := e.walkTask(taskfile.Call{Task: d.Task, Vars: call.Vars.Merge(d.Vars)}, visit)

		if err != nil {
			return err
		}
	}

	for _, c := range task.Cmds {
		if c.Task != "" {
			err := e.walkTask(taskfile.Call{Task: c.Task, Vars: call.Vars.Merge(c.Vars)}, visit)
			if err != nil {
				return err
			}
		}
	}

	err = visit(task)

	if err != nil {
		return err
	}

	return nil
}

func getWatchPathsFromGlobs(dir string, globs []string) ([]string, error) {
	var paths []string

	err := status.VisitGlobs(dir, globs, func(glob string, exclude bool) error {
		if !exclude {
			paths = append(paths, glob)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// best effort at generating watch paths
	for i, path := range paths {
		hasDoubleStar := strings.Index(path, "**") >= 0

		metaCharIndexes := []int{
			strings.Index(path, "*"),
			strings.Index(path, "?"),
			strings.Index(path, "["),
			strings.Index(path, "{"),
		}

		sort.Sort(sort.Reverse(sort.IntSlice(metaCharIndexes)))

		minMetaCharIndex := metaCharIndexes[0]

		hasMeta := minMetaCharIndex >= 0

		if hasMeta {
			path = path[:minMetaCharIndex]
		}

		if hasDoubleStar {
			path = filepath.Dir(path) + "/..."
		} else if hasMeta {
			path = filepath.Dir(path)
		}

		paths[i] = path
	}

	return paths, nil
}

func normalizePath(path string) string {
	if !(path == "." || strings.HasPrefix(path, "/") || strings.HasPrefix(path, "./"))  {
		path = "./" + path
	}
	return path
}

func pathIncludes(path string, another string) bool {
	pathRecursive := filepath.Base(path) == "..."
	anotherRecursive := filepath.Base(another) == "..."

	if pathRecursive {
		path = filepath.Dir(path)
	}

	if anotherRecursive {
		another = filepath.Dir(path)
	}

	path = normalizePath(path)
	another = normalizePath(another)

	if pathRecursive {
		return strings.HasPrefix(another, path)
	} else {
		return path == normalizePath(filepath.Dir(another)) && !anotherRecursive
	}
}
func reduceWatchPaths(paths []string) []string {
	resultPathMap := make(map[string]void)

	for _, p := range paths {
		include := true

		if p == "" {
			continue
		}

		for r := range resultPathMap {
			if pathIncludes(r, p) {
				include = false
				break
			} else if pathIncludes(p, r) {
				delete(resultPathMap, r)
			}
		}

		if include {
			resultPathMap[p] = void{}
		}
	}

	var results []string

	for r := range resultPathMap {
		results = append(results, r)
	}

	return results
}


func (e *Executor) getTaskWatchPaths(call taskfile.Call) ([]string, error) {
	watchedPaths := make(map[string]void)

	err := e.walkTask(call, func(task *taskfile.Task) error {

		files, err := getWatchPathsFromGlobs(task.Dir, task.Sources)
		for _, f := range files {
			watchedPaths[f] = void{}
		}

		if err != nil {
			e.Logger.Errf("task: Unable to determine watch paths for %s: sources: %v, %v", task.Task, task.Sources, err)
			return err
		}

		return nil

	})

	if err != nil {
		return nil, err
	}

	var paths []string

	for path := range watchedPaths {
		paths = append(paths, path)
	}

	paths = reduceWatchPaths(paths)

	sort.Sort(sort.Reverse(sort.StringSlice(paths)))

	return paths, nil
}

func (e *Executor) isTaskDependency(call taskfile.Call, path string) bool {
	if e.isIgnored(path) {
		return false
	}

	var dependencies = make(map[string]void)
	var generated []string

	err := e.walkTask(call, func(task *taskfile.Task) error {
		dir, err := filepath.Abs(task.Dir)
		if err != nil {
			e.Logger.Errf("task: Unable to resolve directory %v", task.Dir)
			return err
		}
		files, err := status.Glob(dir, task.Sources)
		if err != nil {
			return err
		}

		for _, f := range files {
			dependencies[f] = void{}
		}

		files, err = status.Glob(dir, task.Generates)

		if err != nil {
			return err
		}

		generated = append(generated, files...)

		return nil
	})

	for _, f := range generated {
		delete(dependencies, f)
	}

	if err != nil {
		e.Logger.Errf("task: Unable to determine whether %s is a dependency for %v", path, call)
		return false
	} else {
		_, ok := dependencies[path]
		return ok
	}
}

type watcher struct {
	events chan notify.EventInfo
	mu sync.Mutex
	watchPaths []string
}

func (r *watcher) rewatchPaths(e *Executor, call taskfile.Call, watchPaths []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.watchPaths = watchPaths

	r.reallyClose()

	r.events = make(chan notify.EventInfo, 30)

	for _, watchPath := range watchPaths {
		e.Logger.VerboseOutf("task: Watching %s for %s", watchPath, call.Task)
		err := notify.Watch(watchPath, r.events, notify.All)
		if err != nil {
			e.Logger.Errf("task: Unable to watch %s for %s", watchPath, call.Task)
		}
	}

	return nil
}

func (r *watcher)rewatch(e *Executor, call taskfile.Call) error {
	watchPaths, err := e.getTaskWatchPaths(call)

	if err != nil {
		return err
	}

	shouldRewatch := false

	if len(r.watchPaths) != len(watchPaths) {
		shouldRewatch = true
	} else {
		for i, v := range watchPaths {
			if r.watchPaths[i] != v {
				shouldRewatch = true
				break
			}
		}
	}

	if shouldRewatch {
		return r.rewatchPaths(e, call, watchPaths)
	}

	return nil
}

func (r *watcher) reallyClose() {
	events := r.events
	if events != nil {
		notify.Stop(events)
		close(events)
	}
}

func (r *watcher) close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.reallyClose()
}

func newWatcher() *watcher {
	return &watcher{}
}
// start watching a call
// runs a call, reruns when a dependent file changes
// caller should be able to stop the routine
// caller should be able to know that the routine has completed
func (e *Executor) watchTask(interrupted chan void, call taskfile.Call) error {
	_, cancel := e.runCalls(call)

	w := newWatcher()
	defer w.close()

	e.Logger.Outf("task: Started watching %s", call.Task)

	err := w.rewatch(e, call)
	if err != nil {
		return err
	}

	debounce, cancelDebounce := newDebouncer(debounceTime)

	errc := make(chan error)

	for {
		select {
		case event := <-w.events:
			if event != nil {
				debounce(func() {
					if e.isTaskDependency(call, event.Path()) {
						e.Logger.VerboseOutf("task: Triggering rerun of %v due to event %v", call.Task, event)

						cancel()
						if e.Taskfile.ResetVarsOnRerun {
							e.Compiler.Reset()
						}
						_, cancel = e.runCalls(call)

						err = w.rewatch(e, call)
						if err != nil {
							errc <- err
						}
					}
				})
			}
		case err := <- errc:
			cancel()
			cancelDebounce()
			return err
		case <-interrupted:
			cancel()
			cancelDebounce()
			return nil
		}
	}
}

// watchTasks start watching the given tasks
func (e *Executor) watchTasks(calls ...taskfile.Call) error {
	interrupted := make(chan void)
	closeOnInterrupt(interrupted)

	wg := sync.WaitGroup{}

	watchTask := func(call taskfile.Call) {
		err := e.watchTask(interrupted, call)
		if err != nil {
			e.Logger.Errf("task: Unable to watch task %s: %v", call.Task, err)
		}
		wg.Done()
	}

	for _, call := range calls {
		wg.Add(1)
		go watchTask(call)
	}

	wg.Wait()
	return nil
}

type debouncer struct {
	mu    sync.Mutex
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

