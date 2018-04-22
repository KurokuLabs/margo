package mg

import (
	"io"
)

// NewTestingAgent creates a new agent for testing
//
// The agent config used is equivalent to:
// * Codec: DefaultCodec
// * Stdin: stdin or NopReadWriteCloser{} if nil
// * Stdout: stdout or NopReadWriteCloser{} if nil
// * Stderr: NopReadWriteCloser{}
func NewTestingAgent(stdout io.WriteCloser, stdin io.ReadCloser) *Agent {
	if stdout == nil {
		stdout = NopReadWriteCloser{}
	}
	if stdin == nil {
		stdin = NopReadWriteCloser{}
	}
	ag, _ := NewAgent(AgentConfig{
		Stdout: stdout,
		Stdin:  stdin,
		Stderr: NopReadWriteCloser{},
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
