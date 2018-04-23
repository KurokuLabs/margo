package mg

import (
	"context"
	"fmt"
	"github.com/ugorji/go/codec"
	"go/build"
	"margo.sh/misc/pprof/pprofdo"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	ErrNoSettings = fmt.Errorf("no editor settings")

	_ context.Context = (*Ctx)(nil)
)

// Ctx holds data about the current request/reduction.
//
// To create a new instance, use Store.NewCtx()
//
// NOTE: Ctx should be treated as readonly and users should not assign to any
// of its fields or the fields of any of its members.
// If a field must be updated, you should use one of the methods like Copy
//
// Apart from Action and Parent, no other field will ever be nil
// and if updates, no field should be set to nil
type Ctx struct {
	// State is the current state of the world
	*State

	// Action is the action that was dispatched.
	// It's a hint telling reducers about some action that happened,
	// e.g. that the view is about to be saved or that it was changed.
	Action Action

	// Store is the global store
	Store *Store

	// Log is the global logger
	Log *Logger

	// Parent, if set, is the Ctx that this object was copied from
	Parent *Ctx

	doneC      chan struct{}
	cancelOnce *sync.Once
	handle     codec.Handle
}

// newCtx creates a new Ctx
// if st is nil, the state will be set to the equivalent of Store.state.new()
func newCtx(sto *Store, st *State, act Action) *Ctx {
	if st == nil {
		st = sto.state.new()
	}
	return &Ctx{
		State:  st,
		Action: act,

		Store: sto,

		Log: sto.ag.Log,

		doneC:      make(chan struct{}),
		cancelOnce: &sync.Once{},

		handle: sto.ag.handle,
	}
}

// Deadline implements context.Context.Deadline
func (*Ctx) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

// Cancel cancels the ctx by arranging for the Ctx.Done() channel to be closed.
// Canceling this Ctx cancels all other Ctxs Copy()ed from it.
func (mx *Ctx) Cancel() {
	mx.cancelOnce.Do(func() {
		close(mx.doneC)
	})
}

// Done implements context.Context.Done()
func (mx *Ctx) Done() <-chan struct{} {
	return mx.doneC
}

// Err implements context.Context.Err()
func (mx *Ctx) Err() error {
	select {
	case <-mx.Done():
		return context.Canceled
	default:
		return nil
	}
}

// Value implements context.Context.Value() but always returns nil
func (mx *Ctx) Value(k interface{}) interface{} {
	return nil
}

// AgentName returns the name of the agent if set
// if set, it's usually the agent name as used in the command `margo.sh [run...] $agent`
func (mx *Ctx) AgentName() string {
	return mx.Store.ag.Name
}

// ActionIs returns true if the type Ctx.Action is the same type as any of those in actions
func (mx *Ctx) ActionIs(actions ...Action) bool {
	typ := reflect.TypeOf(mx.Action)
	for _, act := range actions {
		if reflect.TypeOf(act) == typ {
			return true
		}
	}
	return false
}

// LangIs is a wrapper around Ctx.View.Lang()
func (mx *Ctx) LangIs(names ...string) bool {
	return mx.View.LangIs(names...)
}

// Copy create a shallow copy of the Ctx.
//
// It calls the functions in updaters on the new object.
// Updating the new Ctx via these functions is preferred to assigning to the new Ctx
func (mx *Ctx) Copy(updaters ...func(*Ctx)) *Ctx {
	x := *mx
	x.Parent = mx
	mx = &x

	for _, f := range updaters {
		f(mx)
	}
	return mx
}

// Begin stars a new task and returns its ticket
func (mx *Ctx) Begin(t Task) *TaskTicket {
	return mx.Store.Begin(t)
}

type Reducer interface {
	Reduce(*Ctx) *State
}

type ReducerList []Reducer

func (rl ReducerList) ReduceCtx(mx *Ctx) *Ctx {
	for _, r := range rl {
		var st *State
		pprofdo.Do(mx, rl.labels(r), func(context.Context) {
			st = r.Reduce(mx)
		})
		mx = mx.Copy(func(mx *Ctx) {
			mx.State = st
		})
	}
	return mx
}

func (rl ReducerList) labels(r Reducer) []string {
	lbl := ""
	if rf, ok := r.(ReduceFunc); ok {
		lbl = rf.Label
	} else {
		lbl = reflect.TypeOf(r).String()
	}
	return []string{"margo.reduce", lbl}
}

func (rl ReducerList) Reduce(mx *Ctx) *State {
	return rl.ReduceCtx(mx).State
}

func (rl ReducerList) Add(reducers ...Reducer) ReducerList {
	return append(rl[:len(rl):len(rl)], reducers...)
}

type ReduceFunc struct {
	Func  func(*Ctx) *State
	Label string
}

func (rf ReduceFunc) Reduce(mx *Ctx) *State {
	return rf.Func(mx)
}

func Reduce(f func(*Ctx) *State) ReduceFunc {
	_, fn, line, _ := runtime.Caller(1)
	for _, gp := range strings.Split(build.Default.GOPATH, string(filepath.ListSeparator)) {
		s := strings.TrimPrefix(fn, filepath.Clean(gp)+string(filepath.Separator))
		if s != fn {
			fn = filepath.ToSlash(s)
			break
		}
	}
	return ReduceFunc{
		Func:  f,
		Label: fmt.Sprintf("%s:%d", fn, line),
	}
}

type EditorProps struct {
	Name    string
	Version string

	handle   codec.Handle
	settings codec.Raw
}

func (ep *EditorProps) Settings(v interface{}) error {
	if ep.handle == nil || len(ep.settings) == 0 {
		return ErrNoSettings
	}
	return codec.NewDecoderBytes(ep.settings, ep.handle).Decode(v)
}

type EditorConfig interface {
	EditorConfig() interface{}
	EnabledForLangs(langs ...string) EditorConfig
}

// StickyState is state that's persisted from one reduction to the next.
// It's holds the current state of the editor.
type StickyState struct {
	View   *View
	Env    EnvMap
	Editor EditorProps
	Config EditorConfig
}

// State holds data about the state of the editor,
// and transformations made by reducers
type State struct {
	StickyState
	Status        StrSet
	Errors        StrSet
	Completions   []Completion
	Tooltips      []Tooltip
	Issues        IssueSet
	clientActions []clientActionType
	BuiltinCmds   BultinCmdList
	UserCmds      []UserCmd
}

func newState(sto *Store) *State {
	return &State{
		StickyState: StickyState{View: newView(sto)},
	}
}

func (st *State) new() *State {
	return &State{StickyState: st.StickyState}
}

func (st *State) Copy(updaters ...func(*State)) *State {
	x := *st
	for _, f := range updaters {
		f(&x)
	}
	return &x
}

func (st *State) AddStatusf(format string, a ...interface{}) *State {
	return st.AddStatus(fmt.Sprintf(format, a...))
}

func (st *State) AddStatus(l ...string) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		st.Status = st.Status.Add(l...)
	})
}

func (st *State) Errorf(format string, a ...interface{}) *State {
	return st.AddError(fmt.Errorf(format, a...))
}

func (st *State) AddError(l ...error) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		for _, e := range l {
			if e != nil {
				st.Errors = st.Errors.Add(e.Error())
			}
		}
	})
}

func (st *State) SetConfig(c EditorConfig) *State {
	return st.Copy(func(st *State) {
		st.Config = c
	})
}

func (st *State) SetSrc(src []byte) *State {
	return st.Copy(func(st *State) {
		st.View = st.View.SetSrc(src)
	})
}

func (st *State) AddCompletions(l ...Completion) *State {
	return st.Copy(func(st *State) {
		st.Completions = append(st.Completions[:len(st.Completions):len(st.Completions)], l...)
	})
}

func (st *State) AddTooltips(l ...Tooltip) *State {
	return st.Copy(func(st *State) {
		st.Tooltips = append(st.Tooltips[:len(st.Tooltips):len(st.Tooltips)], l...)
	})
}

func (st *State) AddIssues(l ...Issue) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		st.Issues = st.Issues.Add(l...)
	})
}

func (st *State) AddBuiltinCmds(l ...BultinCmd) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		st.BuiltinCmds = append(st.BuiltinCmds[:len(st.BuiltinCmds):len(st.BuiltinCmds)], l...)
	})
}

func (st *State) AddUserCmds(l ...UserCmd) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		st.UserCmds = append(st.UserCmds[:len(st.UserCmds):len(st.UserCmds)], l...)
	})
}

func (st *State) addClientActions(l ...clientAction) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		el := make([]clientActionType, 0, len(st.clientActions)+len(l))
		el = append(el, st.clientActions...)
		for _, ca := range l {
			el = append(el, ca.clientAction())
		}
		st.clientActions = el
	})
}

type clientProps struct {
	Editor struct {
		EditorProps
		Settings codec.Raw
	}
	Env  EnvMap
	View *View
}

func (cp *clientProps) finalize(ag *Agent) {
	ce := &cp.Editor
	ep := &cp.Editor.EditorProps
	ep.handle = ag.handle
	ep.settings = ce.Settings
}

func makeClientProps(kvs KVStore) clientProps {
	return clientProps{
		Env:  EnvMap{},
		View: newView(kvs),
	}
}
