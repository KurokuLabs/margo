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
	// ErrNoSettings is the error returned from EditorProps.Settings()
	// when there was no settings sent from the editor
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
// It applies the functions in updaters to the new object.
// Updating the new Ctx via these functions is preferred to assigning to the new object
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

// A Reducer is the main method of state transitions in margo.
// It takes as input a Ctx describing the current state of the world
// and an Action describing some action that happened.
// Based on this action, the reducer returns a new state of the world.
//
// Reducers are called sequentially in the order they were registered
// with Store.Before(), Store.Use() or Store.After().
//
// A reducer should not call Store.State().
//
// Reducers should complete their work as quickly as possible,
// ideally only updating the state and not doing any work in the reducer itself.
// If a reducer is slow it might block the editor UI because some actions like
// fmt'ing the view must wait for the new src before the user
// can continue editing or saving the file.
//
// e.g. during the ViewFmt or ViewPreSave action, a reducer that knows how to
// fmt the file might update the state to hold a fmt'd copy of the view's src.
//
// or it can implement a linter that kicks off a goroutine to try to compile
// a package when one of its files are saved.
//
// The Reduce() function can be used to convert a function to a reducer.
type Reducer interface {
	Reduce(*Ctx) *State
}

// reducerList is a slice of reducers
type reducerList []Reducer

// ReduceCtx calls the reducers in the slice in order.
// For each reducer ran, it adds a ReducerProfile to State.Profiles.
// Additionally, each reducer is ran through pprofdo.Do with prefix label "margo.reduce"
func (rl reducerList) ReduceCtx(mx *Ctx) *Ctx {
	for _, r := range rl {
		pf := ReducerProfile{
			Action: mx.Action,
			Label:  rl.label(r),
			Start:  time.Now(),
		}
		var st *State
		pprofdo.Do(mx, []string{"margo.reduce", pf.Label}, func(context.Context) {
			st = r.Reduce(mx)
		})
		pf.End = time.Now()
		mx = mx.Copy(func(mx *Ctx) {
			mx.State = st.Copy(func(st *State) {
				l := st.Profiles
				st.Profiles = append(l[:len(l):len(l)], pf)
			})
		})
	}
	return mx
}

func (rl reducerList) label(r Reducer) string {
	if r, ok := r.(ReducerLabeler); ok {
		return r.ReducerLabel()
	}
	return reflect.TypeOf(r).String()
}

// Reduce is the equivalent of calling ReduceCtx().State
func (rl reducerList) Reduce(mx *Ctx) *State {
	return rl.ReduceCtx(mx).State
}

// Add adds new reducers to the list. It returns a new list.
func (rl reducerList) Add(reducers ...Reducer) reducerList {
	return append(rl[:len(rl):len(rl)], reducers...)
}

// ReduceFunc wraps a function to be used as a reducer
// New instances should ideally be created using the global Reduce() function
type ReduceFunc struct {
	// Func is the function to be used for the reducer
	Func func(*Ctx) *State

	// Label is an optional string that may be used as a pprof label.
	// If unset, a name based on the Func type will be used.
	Label string
}

// ReducerLabel implements ReducerLabeler
func (rf ReduceFunc) ReducerLabel() string {
	if s := rf.Label; s != "" {
		return s
	}
	return reflect.TypeOf(rf).String()
}

// Reduce implements the Reducer interface, delegating to ReduceFunc.Func
func (rf ReduceFunc) Reduce(mx *Ctx) *State {
	return rf.Func(mx)
}

// Reduce converts a function to a reducer.
// It uses a suitable label based on the file and line on which it is called.
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

// EditorProps holds data about the text editor
type EditorProps struct {
	// Name is the name of the editor
	Name string

	// Version is the editor's version
	Version string

	handle   codec.Handle
	settings codec.Raw
}

// Settings unmarshals the internal settings sent from the editor into v.
// If no settings were sent, it returns ErrNoSettings,
// otherwise it returns any error from unmarshalling.
func (ep *EditorProps) Settings(v interface{}) error {
	if ep.handle == nil || len(ep.settings) == 0 {
		return ErrNoSettings
	}
	return codec.NewDecoderBytes(ep.settings, ep.handle).Decode(v)
}

// EditorConfig is the common interface between internally supported editors.
//
// The main implementation is `sublime.Config`
type EditorConfig interface {
	// EditorConfig returns data to be sent to the editor.
	EditorConfig() interface{}

	// EnabledForLangs is a hint to the editor listing the languages
	// for which actions should be dispatched.
	//
	// To request actions for all languages, use `"*"` (the default)
	EnabledForLangs(langs ...string) EditorConfig
}

// StickyState is state that's persisted from one reduction to the next.
// It holds the current state of the editor.
//
// All fields are readonly and should only be assigned to during a call to State.Copy().
// Child fields esp. View should not be assigned to.
type StickyState struct {
	// View describes the current state of the view.
	// When constructed correctly (through Store.NewCtx()), View is never nil.
	View *View

	// Env holds environment variables sent from the editor.
	// For "go" views in the "margo.sh" tree and "margo" package,
	// "GOPATH" is set to the GOPATH that was used to build the agent.
	Env EnvMap

	// Editor holds data about the editor
	Editor EditorProps

	// Config holds config data for the editor to use
	Config EditorConfig
}

// State holds data about the state of the editor, and transformations made by reducers
//
// All fields are readonly and should only be assigned to during a call to State.Copy()
// Methods on this object that return *State, return a new object.
// As an optimization/implementation details, the methods may choose to return
// the input state object if no updates are done.
//
// New instances can be obtained through Store.NewCtx()
//
// Except StickyState, all fields are cleared at the start of a new dispatch.
// Fields that to be present for some time, e.g. Status and Issues,
// Should be populated at each call to the reducer
// even if the action is not its primary action.
// e.g. for linters, they should kill off a goroutine to do a compilation
// after the file has been saved (ViewSaved) but always return its cached issues.
//
// If a reducer fails to return their state unless their primary action is dispatched
// it could result in flickering in the editor for visible elements like the status
type State struct {
	// StickyState holds the current state of the editor
	StickyState

	// Status holds the list of status messages to show in the view
	Status StrSet

	// Errors hold the list of error to display to the user
	Errors StrSet

	// Completions holds the list of completions to show to the user
	Completions []Completion

	// Issues holds the list of issues to present to the user
	Issues IssueSet

	// BuiltinCmds holds the list of builtin commands.
	// It's usually populated during the RunCmd action.
	BuiltinCmds BultinCmdList

	// UserCmds holds the list of user commands.
	// It's usually populated during the QueryUserCmds action.
	UserCmds []UserCmd

	// Profiles is a list of reducer profiles for reducers that have already ran
	Profiles []ReducerProfile

	// clientActions is a list of client actions to dispatch in the editor
	clientActions []clientActionType
}

// ReducerLabeler is the interface that describes reducers that label themselves
type ReducerLabeler interface {
	Reducer

	// ReducerLabel returns a string that can be used to name the reducer
	// in ReducerProfiles, pprof profiles and other display scenarios
	ReducerLabel() string
}

// ReducerProfile holds details about reducers that are run
type ReducerProfile struct {
	// Label is a string naming the reducer.
	// For reducers that implement ReducerLabeler it's the string that's returned,
	// otherwise it's a string derived from the reducer's type.
	Label string

	// Action is the action that was dispatched
	Action Action

	// Start is the time when the reducer was called
	Start time.Time

	// End is the time when the reducer returned
	End time.Time
}

// newState create a new State object ensuring View is initialized correctly.
func newState(sto *Store) *State {
	return &State{
		StickyState: StickyState{View: newView(sto)},
	}
}

// new creates a new State sharing State.StickyState
func (st *State) new() *State {
	return &State{StickyState: st.StickyState}
}

// Copy create a shallow copy of the State.
//
// It applies the functions in updaters to the new object.
// Updating the new State via these functions is preferred to assigning to the new object
func (st *State) Copy(updaters ...func(*State)) *State {
	x := *st
	st = &x

	for _, f := range updaters {
		f(st)
	}
	return st
}

// AddStatusf is equivalent to State.AddStatus(fmt.Sprintf())
func (st *State) AddStatusf(format string, a ...interface{}) *State {
	return st.AddStatus(fmt.Sprintf(format, a...))
}

// AddStatus adds the list of messages in l to State.Status.
func (st *State) AddStatus(l ...string) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		st.Status = st.Status.Add(l...)
	})
}

// AddErrorf is equivalent to State.AddError(fmt.Sprintf())
func (st *State) AddErrorf(format string, a ...interface{}) *State {
	return st.AddError(fmt.Errorf(format, a...))
}

// AddError adds the non-nil errors in l to State.Errors.
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

// SetConfig updates the State.Config.
func (st *State) SetConfig(c EditorConfig) *State {
	return st.Copy(func(st *State) {
		st.Config = c
	})
}

// SetSrc is a wrapper around View.SetSrc().
// If `len(src) == 0` it does nothing because this is almost always a bug.
func (st *State) SetSrc(src []byte) *State {
	if len(src) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		st.View = st.View.SetSrc(src)
	})
}

// AddCompletions adds the completions in l to State.Completions
func (st *State) AddCompletions(l ...Completion) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		st.Completions = append(st.Completions[:len(st.Completions):len(st.Completions)], l...)
	})
}

// AddIssues adds the list of issues in l to State.Issues
func (st *State) AddIssues(l ...Issue) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		st.Issues = st.Issues.Add(l...)
	})
}

// AddBuiltinCmds adds the list of builtin commands in l to State.BuiltinCmds
func (st *State) AddBuiltinCmds(l ...BultinCmd) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		st.BuiltinCmds = append(st.BuiltinCmds[:len(st.BuiltinCmds):len(st.BuiltinCmds)], l...)
	})
}

// AddUserCmds adds the list of user commands in l to State.userCmds
func (st *State) AddUserCmds(l ...UserCmd) *State {
	if len(l) == 0 {
		return st
	}
	return st.Copy(func(st *State) {
		st.UserCmds = append(st.UserCmds[:len(st.UserCmds):len(st.UserCmds)], l...)
	})
}

// addClientActions adds the list of client actions in l to State.clientActions
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
