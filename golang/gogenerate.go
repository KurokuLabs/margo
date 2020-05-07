package golang

import (
	"margo.sh/golang/goutil"
	"margo.sh/mg"
)

// GoGenerate adds a UserCmd that calls `go generate` in go packages and sub-dirs
type GoGenerate struct {
	mg.ReducerType

	// Args are extra arguments to pass to `go generate`
	Args []string
}

// RCond implements mg.Reducer
func (gg *GoGenerate) RCond(mx *mg.Ctx) bool {
	return mx.ActionIs(mg.QueryUserCmds{}) && goutil.ClosestPkgDir(mx.View.Dir()) != nil
}

// RCond implements mg.Reducer
func (gg *GoGenerate) Reduce(mx *mg.Ctx) *mg.State {
	return mx.State.AddUserCmds(mg.UserCmd{
		Title: "Go Generate",
		Name:  "go",
		Args:  append([]string{"generate"}, gg.Args...),
	})
}
