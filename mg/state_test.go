package mg

import (
	"io"
	"margo.sh/mgutil"
)

// NewTestingAgent creates a new agent for testing
//
// The agent config used is equivalent to:
// * Codec: DefaultCodec
// * Stdin: stdin or &mgutil.IOWrapper{} if nil
// * Stdout: stdout or &mgutil.IOWrapper{} if nil
// * Stderr: &mgutil.IOWrapper{}
func NewTestingAgent(stdout io.WriteCloser, stdin io.ReadCloser) *Agent {
	if stdout == nil {
		stdout = &mgutil.IOWrapper{}
	}
	if stdin == nil {
		stdin = &mgutil.IOWrapper{}
	}
	ag, _ := NewAgent(AgentConfig{
		Stdout: stdout,
		Stdin:  stdin,
		Stderr: &mgutil.IOWrapper{},
	})
	return ag
}

// NewTestingStore creates a new Store for testing
// It's equivalent to NewTestingAgent().Store
func NewTestingStore() *Store {
	return NewTestingAgent(nil, nil).Store
}

// NewTestingCtx creates a new Ctx for testing
// It's equivalent to NewTestingStore().NewCtx()
func NewTestingCtx(act Action) *Ctx {
	return NewTestingStore().NewCtx(act)
}
