package mg

import (
	"fmt"
	"sync"
)

type Reducer func(State, Action) State

type Listener func(State)

type Store struct {
	mu        sync.Mutex
	state     State
	listeners []*struct{ Listener }
	listener  Listener
	reducers  []Reducer
	cfg       func() EditorConfig
}

func (sto *Store) Dispatch(act Action) {
	go sto.dispatch(act)
}

func (sto *Store) dispatch(act Action) {
	sto.mu.Lock()
	defer sto.mu.Unlock()

	sto.reduce(act, true, sto.prepState(sto.state))
}

func (sto *Store) syncRq(ag *Agent, rq AgentReq) {
	sto.mu.Lock()
	defer sto.mu.Unlock()

	initSt := sto.state
	name := rq.Action.Name
	act := ag.createAction(name)

	rs := AgentRes{Cookie: rq.Cookie}
	rs.State.State = initSt
	defer func() { ag.send(rs) }()

	if act == nil {
		rs.Error = fmt.Sprintf("unknown client action: %s", name)
		return
	}

	// TODO: add support for unpacking Action.Data

	st := rq.Props.updateState(sto.prepState(initSt))
	rs.State.State = sto.reduce(act, false, st)
}

func (sto *Store) reduce(act Action, callListener bool, st State) State {
	for _, r := range sto.reducers {
		st = r(st, act)
	}

	if callListener && sto.listener != nil {
		sto.listener(st)
	}

	for _, p := range sto.listeners {
		p.Listener(st)
	}

	sto.state = st

	return st
}

func (sto *Store) State() State {
	sto.mu.Lock()
	defer sto.mu.Unlock()

	return sto.state
}

func (sto *Store) prepState(st State) State {
	st.EphemeralState = EphemeralState{}
	if sto.cfg != nil {
		st.Config = sto.cfg()
	}
	return st
}

func newStore(l Listener) *Store {
	return &Store{listener: l}
}

func (sto *Store) Subscribe(l Listener) (unsubscribe func()) {
	sto.mu.Lock()
	defer sto.mu.Unlock()

	p := &struct{ Listener }{l}
	sto.listeners = append(sto.listeners[:len(sto.listeners):len(sto.listeners)], p)

	return func() {
		sto.mu.Lock()
		defer sto.mu.Unlock()

		listeners := make([]*struct{ Listener }, 0, len(sto.listeners)-1)
		for _, q := range sto.listeners {
			if p != q {
				listeners = append(listeners, q)
			}
		}
		sto.listeners = listeners
	}
}

func (sto *Store) Use(reducers ...Reducer) *Store {
	sto.mu.Lock()
	defer sto.mu.Unlock()

	sto.reducers = append(sto.reducers[:len(sto.reducers):len(sto.reducers)], reducers...)
	return sto
}

func (sto *Store) EditorConfig(f func() EditorConfig) *Store {
	sto.mu.Lock()
	defer sto.mu.Unlock()

	sto.cfg = f
	return sto
}
