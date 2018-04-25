package mg

import (
	"go/build"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	defaultReducers = struct {
		before, use, after []Reducer
	}{
		before: []Reducer{
			&issueKeySupport{},
			Builtins,
		},
		after: []Reducer{
			issueStatusSupport{},
			&cmdSupport{},
			&restartSupport{},
		},
	}
)

type rsBuildRes struct {
	ActionType
	issues IssueSet
}

type restartSupport struct {
	issues IssueSet
}

func (r *restartSupport) Reduce(mx *Ctx) *State {
	st := mx.State
	switch act := mx.Action.(type) {
	case ViewSaved:
		r.tryPrepRestart(mx)
	case Restart:
		mx.Log.Printf("%T action dispatched\n", mx.Action)
		st = mx.addClientActions(clientRestart)
	case Shutdown:
		mx.Log.Printf("%T action dispatched\n", mx.Action)
		st = mx.addClientActions(clientShutdown)
	case rsBuildRes:
		r.issues = act.issues
	}
	return st.AddIssues(r.issues...)
}

func (r *restartSupport) tryPrepRestart(mx *Ctx) {
	v := mx.View
	hasSfx := strings.HasSuffix
	if !hasSfx(v.Path, ".go") || hasSfx(v.Path, "_test.go") {
		return
	}

	dir := filepath.ToSlash(mx.View.Dir())
	if !filepath.IsAbs(dir) {
		return
	}

	// if we use build..ImportPath, it will be wrong if we work on the code outside the GS GOPATH
	imp := ""
	if i := strings.LastIndex(dir, "/src/"); i >= 0 {
		imp = dir[i+5:]
	}
	if imp != "margo" && !strings.HasPrefix(imp+"/", "margo.sh/") {
		return
	}

	go r.prepRestart(mx, dir)
}

func (r *restartSupport) prepRestart(mx *Ctx, dir string) {

	pkg, _ := build.Default.ImportDir(dir, 0)
	if pkg == nil || pkg.Name == "" {
		return
	}

	defer mx.Begin(Task{Title: "prepping margo restart"}).Done()

	cmd := exec.Command("margo.sh", "build", mx.AgentName())
	cmd.Dir = mx.View.Dir()
	cmd.Env = mx.Env.Environ()
	out, err := cmd.CombinedOutput()

	iw := &IssueWriter{
		Dir:      mx.View.Dir(),
		Patterns: CommonPatterns,
		Base:     Issue{Label: "Mg/RestartSupport"},
	}
	iw.Write(out)
	iw.Flush()
	res := rsBuildRes{issues: iw.Issues()}

	msg := "telling margo to restart after " + mx.View.Filename() + " was saved"
	if err == nil && len(res.issues) == 0 {
		mx.Log.Println(msg)
		mx.Store.Dispatch(Restart{})
	} else {
		mx.Log.Printf("not %s: `margo.sh build %s` failed: error: %v\n%s\n", msg, mx.AgentName(), err, out)
		mx.Store.Dispatch(res)
	}
}
