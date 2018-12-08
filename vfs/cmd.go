package vfs

import (
	"margo.sh/mg"
)

type vfsCmd struct{ mg.ReducerType }

func (vc *vfsCmd) Reduce(mx *mg.Ctx) *mg.State {
	if rc, ok := mx.Action.(mg.RunCmd); ok && rc.Name == ".vfs" {
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

func init() {
	mg.DefaultReducers.Use(&vfsCmd{})
}
