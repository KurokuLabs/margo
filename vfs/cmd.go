package vfs

import (
	"margo.sh/mg"
	"path/filepath"
)

type vfsCmd struct{ mg.ReducerType }

func (vc *vfsCmd) Reduce(mx *mg.Ctx) *mg.State {
	switch mx.Action.(type) {
	case mg.ViewSaved:
		go vc.saved(mx)
	case mg.RunCmd:
		return mx.AddBuiltinCmds(mg.BuiltinCmd{
			Name: ".vfs",
			Desc: "Print a tree representing vfs.Root",
			Run:  vc.run,
		})
	}
	return mx.State
}

func (vc *vfsCmd) run(cx *mg.CmdCtx) *mg.State {
	go vc.cmd(cx)
	return cx.State
}

func (vc *vfsCmd) cmd(cx *mg.CmdCtx) {
	defer cx.Output.Close()

	if len(cx.Args) == 0 {
		Root.Print(cx.Output)
		return
	}

	for _, p := range cx.Args {
		nd, pat := &Root.Node, p
		if filepath.IsAbs(p) {
			nd, pat = Root.Peek(filepath.Dir(p)), filepath.Base(p)
		}
		nd.PrintWithFilter(cx.Output, func(nd *Node) string {
			if nd.IsBranch() {
				return nd.String()
			}
			if ok, _ := filepath.Match(pat, nd.Name()); ok {
				return nd.String()
			}
			return ""
		})
	}
}

func (vc *vfsCmd) saved(mx *mg.Ctx) {
	v := mx.View
	if v.Path == "" {
		return
	}
	Root.Sync(v.Dir())
	Root.Sync(v.Path)
}

func init() {
	mg.DefaultReducers.Before(&vfsCmd{})
}
