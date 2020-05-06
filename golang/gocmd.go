package golang

import (
	"bytes"
	"fmt"
	"github.com/dustin/go-humanize"
	"go/ast"
	"go/build"
	"io"
	"io/ioutil"
	"margo.sh/golang/cursor"
	"margo.sh/mg"
	"margo.sh/mgutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type GoCmd struct {
	mg.ReducerType

	Humanize bool
}

func (gc *GoCmd) Reduce(mx *mg.Ctx) *mg.State {
	switch act := mx.Action.(type) {
	case mg.QueryUserCmds:
		return gc.userCmds(mx)
	case mg.RunCmd:
		return gc.runCmd(mx, act)
	}
	return mx.State
}

func (gc *GoCmd) userCmds(mx *mg.Ctx) *mg.State {
	return mx.AddUserCmds(
		mg.UserCmd{
			Name:  "go.play",
			Title: "Go Play",
		},
		mg.UserCmd{
			Name:  "go.replay",
			Title: "Go RePlay (single instance)",
		},
	)
}

func (gc *GoCmd) runCmd(mx *mg.Ctx, rc mg.RunCmd) *mg.State {
	return mx.State.AddBuiltinCmds(
		mg.BuiltinCmd{
			Run:  gc.goBuiltin,
			Name: "go",
			Desc: "Wrapper around the go command, adding linter support",
		},
		mg.BuiltinCmd{
			Run:  gc.playBuiltin,
			Name: "go.play",
			Desc: "Automatically build and run go commands or run go test for packages with support for linting and unsaved files",
		},
		mg.BuiltinCmd{
			Run:  gc.replayBuiltin,
			Name: "go.replay",
			Desc: "Wrapper around go.play limited to a single instance",
		},
	)
}

func (gc *GoCmd) goBuiltin(bx *mg.CmdCtx) *mg.State {
	go gc.goTool(bx)
	return bx.State
}

func (gc *GoCmd) playBuiltin(bx *mg.CmdCtx) *mg.State {
	go gc.playTool(bx, "")
	return bx.State
}

func (gc *GoCmd) replayBuiltin(bx *mg.CmdCtx) *mg.State {
	v := bx.View
	cid := ""
	if v.Path == "" {
		cid = v.Name
	} else {
		cid = v.Dir()
	}
	go gc.playTool(bx, "go.replay`"+cid+"`")
	return bx.State
}

func (gc *GoCmd) goTool(bx *mg.CmdCtx) {
	gx := newGoCmdCtx(gc, bx, "go.builtin", "", "", "")
	defer gx.Output.Close()
	gx.run(gx.View)
}

func (gc *GoCmd) playTool(bx *mg.CmdCtx, cancelID string) {
	origView := bx.View
	bx, tDir, tFn, err := gc.playTempDir(bx)
	gx := newGoCmdCtx(gc, bx, "go.play", cancelID, tDir, tFn)
	defer gx.Output.Close()

	gx.Verbose = true

	if err != nil {
		fmt.Fprintf(gx.Output, "Error: %s\n", err)
	}
	if tDir == "" {
		return
	}
	defer os.RemoveAll(tDir)

	bld := BuildContext(gx.Ctx)
	pkg, err := bld.ImportDir(gx.pkgDir, 0)
	switch {
	case err != nil:
		fmt.Fprintln(gx.Output, "Error: cannot import package:", err)
	case !pkg.IsCommand() || strings.HasSuffix(bx.View.Filename(), "_test.go"):
		gc.playToolTest(gx, bld, origView)
	default:
		gc.playToolRun(gx, bld, origView)
	}
}

func (gc *GoCmd) playTempDir(bx *mg.CmdCtx) (newBx *mg.CmdCtx, tDir string, tFn string, err error) {
	tDir, err = mg.MkTempDir("go.play")
	if err != nil {
		return bx, "", "", fmt.Errorf("cannot MkTempDir: %s", err)
	}

	if !bx.LangIs(mg.Go) {
		return bx, tDir, "", nil
	}

	v := bx.View
	if v.Path != "" {
		return bx, tDir, tFn, nil
	}

	tFn = filepath.Join(tDir, v.Name)
	src, err := v.ReadAll()
	if err == nil {
		err = ioutil.WriteFile(tFn, src, 0600)
	}
	if err != nil {
		return bx, tDir, "", fmt.Errorf("cannot create temp file: %s", err)
	}

	bx = bx.Copy(func(bx *mg.CmdCtx) {
		bx.Ctx = bx.Ctx.Copy(func(mx *mg.Ctx) {
			mx.State = mx.State.Copy(func(st *mg.State) {
				st.View = st.View.Copy(func(v *mg.View) {
					v.Path = tFn
				})
			})
		})
	})

	return bx, tDir, tFn, nil
}

func (gc *GoCmd) playToolTest(gx *goCmdCtx, bld *build.Context, origView *mg.View) {
	argsPfx := []string{"test", "-test.run=."}
	cx := cursor.NewViewCurCtx(gx.Ctx)
	for _, n := range cx.Nodes {
		x, ok := n.(*ast.FuncDecl)
		if !ok || x.Name == nil {
			continue
		}
		nm := x.Name.String()
		if strings.HasPrefix(nm, "Benchmark") {
			argsPfx = append(argsPfx, "-test.bench=^"+nm+"$")
		}
	}
	gx.Args = append(argsPfx, gx.Args...)
	if origView.Path == "" {
		gx.Args = append(gx.Args, gx.tFn)
	}
	gx.run(origView)
}

func (gc *GoCmd) playToolRun(gx *goCmdCtx, bld *build.Context, origView *mg.View) {
	nm := filepath.Base(origView.Name)
	if origView.Path != "" {
		nm = filepath.Base(origView.Dir())
	}

	args := gx.Args
	exe := filepath.Join(gx.tDir, "margo.play~~"+nm+".exe")
	gx.CmdCtx = gx.CmdCtx.Copy(func(bx *mg.CmdCtx) {
		bx.Name = "go"
		bx.Args = []string{"build", "-o", exe}
		bx.Ctx = bx.Ctx.Copy(func(mx *mg.Ctx) {
			mx.State = mx.State.Copy(func(st *mg.State) {
				st.View = st.View.Copy(func(v *mg.View) {
					v.Wd = v.Dir()
				})
			})
		})
	})
	if err := gx.run(origView); err != nil {
		return
	}

	gx.CmdCtx = gx.CmdCtx.Copy(func(bx *mg.CmdCtx) {
		bx.Name = exe
		bx.Args = args
		bx.Ctx = bx.Ctx.Copy(func(mx *mg.Ctx) {
			mx.State = mx.State.Copy(func(st *mg.State) {
				st.View = origView
			})
		})
	})
	gx.RunProc()
}

type goCmdCtx struct {
	*mg.CmdCtx
	pkgDir string
	key    interface{}
	iw     *mg.IssueOut
	tDir   string
	tFn    string
}

func newGoCmdCtx(gc *GoCmd, bx *mg.CmdCtx, label, cancelID string, tDir, tFn string) *goCmdCtx {
	gx := &goCmdCtx{
		pkgDir: bx.View.Dir(),
		tDir:   tDir,
		tFn:    tFn,
	}

	type Key struct{ label string }
	gx.key = Key{label}

	gx.iw = &mg.IssueOut{
		Base:     mg.Issue{Label: label},
		Patterns: bx.CommonPatterns(),
		Dir:      gx.pkgDir,
	}

	output := bx.Output
	if gc.Humanize && len(bx.Args) > 0 && bx.Args[0] == "test" {
		output = mgutil.NewSplitWriter(mgutil.SplitLine, &humanizeWriter{output})
	}

	gx.CmdCtx = bx.Copy(func(bx *mg.CmdCtx) {
		bx.Name = "go"
		bx.CancelID = cancelID
		bx.Output = mg.OutputStreams{
			output,
			gx.iw,
		}
	})

	return gx
}

func (gx *goCmdCtx) run(origView *mg.View) error {
	defer func() {
		gx.VFS.Invalidate(origView.Filename())
		gx.VFS.Invalidate(origView.Dir())
	}()

	p, err := gx.StartProc()
	if err == nil {
		err = p.Wait()
	}
	gx.iw.Flush()

	issues := gx.iw.Issues()
	for i, isu := range issues {
		if isu.Path == gx.tFn {
			isu.Name = origView.Name
			isu.Path = origView.Path
		}
		issues[i] = isu
	}

	ik := mg.IssueKey{Key: gx.key}
	if origView.Path == "" {
		ik.Name = origView.Name
	} else {
		ik.Dir = origView.Dir()
	}

	gx.Store.Dispatch(mg.StoreIssues{IssueKey: ik, Issues: issues})
	return err
}

func isWhiteSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func humanizeMetric(met string) string {
	i := 0
	for i < len(met) && isWhiteSpace(met[i]) {
		i++
	}
	j := i
	for j < len(met) && !isWhiteSpace(met[j]) {
		j++
	}
	pfx := met[:i]
	val := met[i:j]
	sfx := met[j:]
	num, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return met
	}
	switch strings.TrimSpace(sfx) {
	case "ns/op":
		s := time.Duration(num).String()
		i := 0
		for i < len(s) {
			c := s[i]
			if (c >= '0' && c <= '9') || c == '.' {
				i++
			} else {
				break
			}
		}
		return pfx + s[:i] + " " + s[i:] + "/op"
	case "B/op":
		return pfx + humanize.IBytes(uint64(num)) + "/op"
	default:
		return pfx + humanize.Comma(num) + sfx
	}
}

type humanizeWriter struct {
	io.WriteCloser
}

func (w *humanizeWriter) Write(ln []byte) (int, error) {
	s := make([]byte, 0, len(ln)+42)
	for len(ln) != 0 {
		i := bytes.IndexByte(ln, '\t')
		if i < 0 {
			s = append(s, humanizeMetric(string(ln))...)
			break
		}
		i++
		s = append(s, humanizeMetric(string(ln[:i]))...)
		ln = ln[i:]
	}
	return w.WriteCloser.Write(s)
}
