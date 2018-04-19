package mg

import (
	"testing"
)

type nonSpec struct{ ActionType }

func TestCmdSupport_Reduce_noCalls(t *testing.T) {
	cs := &cmdSupport{}
	ag, _ := NewAgent(AgentConfig{})
	ctx, done := ag.NewCtx(nil)
	defer close(done)

	ctx.Action = nil
	if state := cs.Reduce(ctx); state != ag.Store.State() {
		t.Errorf("cmdSupport.Reduce() = %v, want %v", state, ag.Store.State())
	}

	ctx.Action = new(nonSpec)
	if state := cs.Reduce(ctx); state != ag.Store.State() {
		t.Errorf("cmdSupport.Reduce() = %v, want %v", state, ag.Store.State())
	}
}

func TestCmdSupport_Reduce_withRunCmd(t *testing.T) {
	var called bool
	cs := &cmdSupport{}
	ag, _ := NewAgent(AgentConfig{})
	ctx, done := ag.NewCtx(nil)
	defer close(done)

	ctx.Action = RunCmd{
		Fd:   "rHX23",
		Name: ".mytest",
	}
	ctx.State = ctx.AddBuiltinCmds(BultinCmd{
		Name: ".mytest",
		Run: func(*BultinCmdCtx) *State {
			called = true
			return ag.Store.State()
		},
	})

	if state := cs.Reduce(ctx); state != ag.Store.State() {
		t.Errorf("cmdSupport.Reduce() = %v, want %v", state, ag.Store.State())
	}
	if !called {
		t.Errorf("cs.Reduce(%v): cs.runCmd() wasn't called", ctx)
	}
}

func TestCmdSupport_Reduce_withCmdOutput(t *testing.T) {
	var called bool
	fd := "CIlZ7zBWHIAL"
	cs := &cmdSupport{}
	ag, _ := NewAgent(AgentConfig{})
	ctx, done := ag.NewCtx(nil)
	defer close(done)

	ctx.Action = CmdOutput{
		Fd: fd,
	}

	state := cs.Reduce(ctx)
	for _, c := range state.clientActions {
		if d, ok := c.Data.(CmdOutput); ok && d.Fd == fd {
			called = true
			break
		}
	}
	if !called {
		t.Errorf("cs.Reduce(%v): cs.cmdOutput() wasn't called", ctx)
	}
}
