package mg

import (
	"testing"
)

func TestCmdSupport_Reduce_noCalls(t *testing.T) {
	type unknown struct{ ActionType }
	cs := &cmdSupport{}
	ag, _ := NewAgent(AgentConfig{})
	ctx, done := ag.NewCtx(nil)
	defer close(done)

	if state := cs.Reduce(ctx); state != ag.Store.State() {
		t.Errorf("cmdSupport.Reduce() = %v, want %v", state, ag.Store.State())
	}

	ctx.Action = new(unknown)
	if state := cs.Reduce(ctx); state != ctx.State {
		t.Errorf("cmdSupport.Reduce() = %v, want %v", state, ctx.State)
	}
}

func TestCmdSupport_Reduce_withRunCmd(t *testing.T) {
	var called bool
	cs := &cmdSupport{}
	ag, _ := NewAgent(AgentConfig{})
	ctx, done := ag.NewCtx(RunCmd{
		Fd:   "rHX23",
		Name: ".mytest",
	})
	defer close(done)

	ctx.State = ctx.AddBuiltinCmds(BultinCmd{
		Name: ".mytest",
		Run: func(bx *BultinCmdCtx) *State {
			called = true
			return bx.State
		},
	})

	if state := cs.Reduce(ctx); state != ctx.State {
		t.Errorf("cmdSupport.Reduce() = %v, want %v", state, ctx.State)
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
