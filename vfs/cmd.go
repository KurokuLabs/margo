package vfs

import (
	"margo.sh/mg"
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
	Root.Print(cx.Output)
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
	mg.DefaultReducers.Use(&vfsCmd{})
}
