package compat

import (
	"margo.sh/mg"
	"margo.sh/mgutil"
	"runtime"
	"strings"
	"testing"
)

type lifecycleState struct {
	names []string
}

func (ls *lifecycleState) called() {
	pc, _, _, _ := runtime.Caller(1)
	f := runtime.FuncForPC(pc)
	l := strings.Split(f.Name(), ".")
	target := l[len(l)-1]
	for i, s := range ls.names {
		if s == target {
			ls.names = append(ls.names[:i], ls.names[i+1:]...)
			break
		}
	}
}

func (ls *lifecycleState) uncalled() string {
	return strings.Join(ls.names, ", ")
}

func (ls *lifecycleState) test(t *testing.T, r mg.Reducer) {
	t.Helper()

	ag, _ := mg.NewAgent(mg.AgentConfig{
		Stdin:  &mgutil.IOWrapper{Reader: strings.NewReader(`{}`)},
		Stdout: &mgutil.IOWrapper{},
		Stderr: &mgutil.IOWrapper{},
	})
	ag.Store.Use(r)
	ag.Run()
	if s := ls.uncalled(); s != "" {
		t.Fatalf("reduction failed to call: %s", s)
	}
}

type legacyLifecycleEmbedded struct {
	mg.Reducer
}

type lifecycle struct {
	mg.ReducerType
	*lifecycleState
}

func (l *lifecycle) Reduce(mx *mg.Ctx) *mg.State       { l.called(); return mx.State }
func (l *lifecycle) RInit(_ *mg.Ctx)                   { l.called() }
func (l *lifecycle) RConfig(_ *mg.Ctx) mg.EditorConfig { l.called(); return nil }
func (l *lifecycle) RCond(_ *mg.Ctx) bool              { l.called(); return true }
func (l *lifecycle) RMount(_ *mg.Ctx)                  { l.called() }
func (l *lifecycle) RUnmount(_ *mg.Ctx)                { l.called() }

type legacyLifecycle struct {
	mg.ReducerType
	*lifecycleState
}

func (l *legacyLifecycle) Reduce(mx *mg.Ctx) *mg.State             { l.called(); return mx.State }
func (l *legacyLifecycle) ReducerInit(_ *mg.Ctx)                   { l.called() }
func (l *legacyLifecycle) ReducerConfig(_ *mg.Ctx) mg.EditorConfig { l.called(); return nil }
func (l *legacyLifecycle) ReducerCond(_ *mg.Ctx) bool              { l.called(); return true }
func (l *legacyLifecycle) ReducerMount(_ *mg.Ctx)                  { l.called() }
func (l *legacyLifecycle) ReducerUnmount(_ *mg.Ctx)                { l.called() }

type lifecycleEmbedded struct {
	mg.Reducer
}

func TestLifecycleMethodCalls(t *testing.T) {
	names := func() []string {
		return []string{
			"Reduce", "RInit", "RConfig",
			"RCond", "RMount", "RUnmount",
		}
	}
	legacyNames := func() []string {
		return []string{
			"Reduce", "ReducerInit", "ReducerConfig",
			"ReducerCond", "ReducerMount", "ReducerUnmount",
		}
	}
	t.Run("Direct Calls", func(t *testing.T) {
		ls := &lifecycleState{names: names()}
		ls.test(t, &lifecycle{lifecycleState: ls})
	})
	t.Run("Embedded Calls", func(t *testing.T) {
		ls := &lifecycleState{names: names()}
		ls.test(t, &lifecycleEmbedded{&lifecycle{lifecycleState: ls}})
	})
	t.Run("Legacy Direct Calls", func(t *testing.T) {
		ls := &lifecycleState{names: legacyNames()}
		ls.test(t, &legacyLifecycle{lifecycleState: ls})
	})
	t.Run("Legacy Embedded Calls", func(t *testing.T) {
		ls := &lifecycleState{names: legacyNames()}
		ls.test(t, &lifecycleEmbedded{&legacyLifecycle{lifecycleState: ls}})
	})
}
