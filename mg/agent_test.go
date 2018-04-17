package mg

import (
	"os"
	"strings"
	"testing"
)

// TestDefaults tries to verify some assumptions that are, or will be, made throughout the code-base
// the following should hold true regardless of what configuration is exposed in the future
// * the default codec should be json
// * logs should go to os.Stderr by default
// * IPC communication should be done on os.Stdin and os.Stdout by default
func TestDefaults(t *testing.T) {
	ag, err := NewAgent(AgentConfig{
		Codec: "invalidcodec",
	})
	if err == nil {
		t.Error("NewAgent() = (nil); want (error)")
	}
	if ag != nil {
		t.Errorf("ag = (%v); want (nil)", ag)
	}

	ag, err = NewAgent(AgentConfig{})
	if err != nil {
		t.Fatalf("agent creation failed: %s", err)
	}

	stdin := ag.stdin
	if w, ok := stdin.(*LockedReadCloser); ok {
		stdin = w.ReadCloser
	}
	stdout := ag.stdout
	if w, ok := stdout.(*LockedWriteCloser); ok {
		stdout = w.WriteCloser
	}
	stderr := ag.stderr
	if w, ok := stderr.(*LockedWriteCloser); ok {
		stderr = w.WriteCloser
	}

	cases := []struct {
		name   string
		expect interface{}
		got    interface{}
	}{
		{`DefaultCodec == json`, true, DefaultCodec == "json"},
		{`codecHandles[DefaultCodec] exists`, true, codecHandles[DefaultCodec] != nil},
		{`codecHandles[""] == codecHandles[DefaultCodec]`, true, codecHandles[""] == codecHandles[DefaultCodec]},
		{`default Agent.stdin`, os.Stdin, stdin},
		{`default Agent.stdout`, os.Stdout, stdout},
		{`default Agent.stderr`, os.Stderr, stderr},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.expect != c.got {
				t.Errorf("expected '%v', got '%v'", c.expect, c.got)
			}
		})
	}
}

func TestFirstAction(t *testing.T) {
	nrwc := NopReadWriteCloser{
		Reader: strings.NewReader("{}\n"),
	}
	ag, err := NewAgent(AgentConfig{
		Stdin:  nrwc,
		Stdout: nrwc,
		Stderr: nrwc,
	})
	if err != nil {
		t.Fatalf("agent creation failed: %s", err)
	}

	actions := make(chan Action, 1)
	ag.Store.Use(Reduce(func(mx *Ctx) *State {
		select {
		case actions <- mx.Action:
		default:
		}
		return mx.State
	}))

	// there is a small chance that some other package might dispatch an action
	// before we're ready e.g. in init()
	type impossibru struct{ ActionType }
	ag.Store.Dispatch(impossibru{})

	go ag.Run()
	act := <-actions
	switch act.(type) {
	case Started:
	default:
		t.Errorf("Expected first action to be `%T`, but it was %T\n", Started{}, act)
	}
}

type readWriteCloseStub struct {
	NopReadWriteCloser
	closed    bool
	CloseFunc func() error
}

func (r *readWriteCloseStub) Close() error { return r.CloseFunc() }

func TestAgentShutdown(t *testing.T) {
	nrc := &readWriteCloseStub{}
	nwc := &readWriteCloseStub{}
	nerrc := &readWriteCloseStub{}
	nrc.CloseFunc = func() error {
		nrc.closed = true
		return nil
	}
	nwc.CloseFunc = func() error {
		nwc.closed = true
		return nil
	}
	nerrc.CloseFunc = func() error {
		nerrc.closed = true
		return nil
	}

	ag, err := NewAgent(AgentConfig{
		Stdin:  nrc,
		Stdout: nwc,
		Stderr: nerrc,
		Codec:  "msgpack",
	})
	if err != nil {
		t.Fatalf("agent creation: err = (%#v); want (nil)", err)
	}
	ag.Store = newStore(ag, ag.listener)
	err = ag.Run()
	if err != nil {
		t.Fatalf("ag.Run() = (%#v); want (nil)", err)
	}

	if !nrc.closed {
		t.Error("nrc.Close() want not called")
	}
	if !nwc.closed {
		t.Error("nwc.Close() want not called")
	}
	if !nerrc.closed {
		t.Error("nerrc.Close() want not called")
	}
	if !ag.sd.closed {
		t.Error("ag.sd.closed = (true); want (false)")
	}
}
