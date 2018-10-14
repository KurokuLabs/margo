package mg

import (
	"reflect"
	"runtime"
	"sync"
)

var (
	_ Reducer = &reducerType{}

	// DefaultReducers enables the automatic registration of reducers to the Agent's store
	//
	// This can be used to register reducers without user-interaction
	// but where possible, it should not be used.
	//
	// its methods should only be callsed during init()
	// any calls after this may ignored
	DefaultReducers = &defaultReducers{
		before: reducerList{
			&issueKeySupport{},
			Builtins,
		},
		after: reducerList{
			&issueStatusSupport{},
			&cmdSupport{},
			&restartSupport{},
			&clientActionSupport{},
		},
	}
)

type defaultReducers struct {
	mu                 sync.Mutex
	before, use, after reducerList
}

// Before arranges for the reducers in l to be registered when the agent starts
// it's the equivalent of the user manually calling Store.Before(l...)
func (dr *defaultReducers) Before(l ...Reducer) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	dr.before = dr.before.Add(l...)
}

// Use arranges for the reducers in l to be registered when the agent starts
// it's the equivalent of the user manually calling Store.Use(l...)
func (dr *defaultReducers) Use(l ...Reducer) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	dr.use = dr.use.Add(l...)
}

// After arranges for the reducers in l to be registered when the agent starts
// it's the equivalent of the user manually calling Store.After(l...)
func (dr *defaultReducers) After(l ...Reducer) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	dr.after = dr.after.Add(l...)
}

// A Reducer is the main method of state transitions in margo.
//
// The methods are called in the order listed below:
//
// * ReInit
//   this is called during the first action (initAction{} FKA Started{})
//
// * ReEditorConfig
//   this is called on each reduction
//
// * ReCond
//   this is called on each reduction
//   if it returns false, no other method is called
//
// * ReMount
//   this is called once, after the first time ReCond returns true
//
// * Reduce
//   this is called on each reduction until the agent begins shutting down
//
// * ReUnmount
//   this is called once when the agent is shutting down,
//   iif ReMount was called
//
// For simplicity and the ability to extend the interface in the future,
// users should embed `ReducerType` in their types to complete the interface.
//
// For convenience, it also implements all optional (non-Reduce()) methods.
//
// The prefixes `Re`, `Reduce` and `Reducer` are reserved, and should not be used.
//
// NewReducer() can be used to convert a function to a reducer.
//
// For reducers that are backed by goroutines that are only interested
// in the *last* of some value e.g. *Ctx, mgutil.ChanQ might be of use.
type Reducer interface {
	// Reduce takes as input a Ctx describing the current state of the world
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
	//
	// If a reducer is slow it might block the editor UI because some actions like
	// fmt'ing the view must wait for the new src before the user
	// can continue editing or saving the file.
	//
	// e.g. during the ViewFmt or ViewPreSave action, a reducer that knows how to
	// fmt the file might update the state to hold a fmt'd copy of the view's src.
	//
	// or it can implement a linter that kicks off a goroutine to try to compile
	// a package when one of its files when the ViewSaved action is dispatched.
	Reduce(*Ctx) *State

	// ReLabel returns a string that can be used to name the reducer
	// in pf.Profile and other display scenarios
	ReLabel() string

	// ReInit is called for the first reduction
	// * it's only called once and can be used to initialise reducer state
	//   e.g. for initialising an embedded type
	// * it's called before ReEditorConfig()
	ReInit(*Ctx)

	// ReEditorConfig is called on each reduction, before ReCond
	// if it returns a new EditorConfig, it's equivalent to State.SetConfig()
	// but is always run before ReCond() so is usefull for making sure
	// configuration changes are always applied, even if Reduce() isn't called
	ReEditorConfig(*Ctx) EditorConfig

	// ReCond is called before Reduce and ReMount is called
	// if it returns false, no other methods are called
	//
	// It can be used as a pre-condition in combination with Reducer(Un)Mount
	ReCond(*Ctx) bool

	// ReMount is called once, after the first time that ReCond returns true
	ReMount(*Ctx)

	// ReUnmount is called when communication with the client will stop
	// it is only called if ReMount was called
	//
	// It can be used to clean up any resources created in ReMount
	//
	// After this method is called, Reduce will never be called again
	ReUnmount(*Ctx)

	reducerType() *ReducerType
}

type useReducerForThePrefixNotReduce struct{}

type reducerType struct{ ReducerType }

func (rt *reducerType) Reduce(mx *Ctx) *State { return mx.State }

// ReducerType implements all optional methods of a reducer
type ReducerType struct{}

func (rt *ReducerType) reducerType() *ReducerType { return rt }

// ReLabel implements Reducer.ReLabel
func (rt *ReducerType) ReLabel() string { return "" }

// ReInit implements Reducer.ReInit
func (rt *ReducerType) ReInit(*Ctx) {}

// ReCond implements Reducer.ReCond
func (rt *ReducerType) ReCond(*Ctx) bool { return true }

// ReEditorConfig implements Reducer.ReEditorConfig
func (rt *ReducerType) ReEditorConfig(*Ctx) EditorConfig { return nil }

// ReMount implements Reducer.ReMount
func (rt *ReducerType) ReMount(*Ctx) {}

// ReUnmount implements Reducer.ReUnmount
func (rt *ReducerType) ReUnmount(*Ctx) {}

// reducerList is a slice of reducers
type reducerList []Reducer

func (rl reducerList) callReducers(mx *Ctx) *Ctx {
	for _, r := range rl {
		mx = rl.callReducer(mx, r)
	}
	return mx
}

func (rl reducerList) callReducer(mx *Ctx, r Reducer) *Ctx {
	defer mx.Profile.Push(ReducerLabel(r)).Pop()

	reInit(mx, r)

	if c := reEditorConfig(mx, r); c != nil {
		mx = mx.SetState(mx.State.SetConfig(c))
	}

	if !reCond(mx, r) {
		return mx
	}

	reMount(mx, r)

	if reUnmount(mx, r) {
		return mx
	}

	return reReduce(mx, r)
}

func reInit(mx *Ctx, r Reducer) {
	if _, ok := mx.Action.(initAction); !ok {
		return
	}

	defer mx.Profile.Push("ReInit").Pop()

	if x, ok := r.(interface{ ReducerInit(*Ctx) }); ok {
		x.ReducerInit(mx)
		return
	}

	r.ReInit(mx)
}

func reEditorConfig(mx *Ctx, r Reducer) EditorConfig {
	defer mx.Profile.Push("ReEditorConfig").Pop()

	if x, ok := r.(interface{ ReducerConfig(*Ctx) EditorConfig }); ok {
		return x.ReducerConfig(mx)
	}

	return r.ReEditorConfig(mx)
}

func reCond(mx *Ctx, r Reducer) bool {
	defer mx.Profile.Push("ReCond").Pop()

	if x, ok := r.(interface{ ReducerCond(*Ctx) bool }); ok {
		return x.ReducerCond(mx)
	}

	return r.ReCond(mx)
}

func reMount(mx *Ctx, r Reducer) {
	k := r.reducerType()
	if mx.Store.mounted[k] {
		return
	}

	defer mx.Profile.Push("Mount").Pop()
	mx.Store.mounted[k] = true

	if x, ok := r.(interface{ ReducerMount(*Ctx) }); ok {
		x.ReducerMount(mx)
		return
	}

	r.ReMount(mx)
}

func reUnmount(mx *Ctx, r Reducer) bool {
	k := r.reducerType()
	if !mx.ActionIs(unmount{}) || !mx.Store.mounted[k] {
		return false
	}
	defer mx.Profile.Push("Unmount").Pop()
	delete(mx.Store.mounted, k)

	if x, ok := r.(interface{ ReducerUnmount(*Ctx) }); ok {
		x.ReducerUnmount(mx)
	} else {
		r.ReUnmount(mx)
	}

	return true
}

func reReduce(mx *Ctx, r Reducer) *Ctx {
	defer mx.Profile.Push("Reduce").Pop()
	return mx.SetState(r.Reduce(mx))
}

// Add adds new reducers to the list. It returns a new list.
func (rl reducerList) Add(reducers ...Reducer) reducerList {
	return append(rl[:len(rl):len(rl)], reducers...)
}

// ReduceFunc wraps a function to be used as a reducer
// New instances should ideally be created using the global NewReducer() function
type ReduceFunc struct {
	ReducerType

	// Func is the function to be used for the reducer
	Func func(*Ctx) *State

	// Label is an optional string that may be used as a name for the reducer.
	// If unset, a name based on the Func type will be used.
	Label string
}

// ReLabel implements Reducer.ReLabel
func (rf *ReduceFunc) ReLabel() string {
	if s := rf.Label; s != "" {
		return s
	}
	nm := ""
	if p := runtime.FuncForPC(reflect.ValueOf(rf.Func).Pointer()); p != nil {
		nm = p.Name()
	}
	return "mg.Reduce(" + nm + ")"
}

// Reduce implements the Reducer interface, delegating to ReduceFunc.Func
func (rf *ReduceFunc) Reduce(mx *Ctx) *State {
	return rf.Func(mx)
}

// NewReducer creates a new ReduceFunc
func NewReducer(f func(*Ctx) *State) *ReduceFunc {
	return &ReduceFunc{Func: f}
}

// ReducerLabel returns a label for the reducer r.
// It takes into account the Reducer.ReLabel method.
func ReducerLabel(r Reducer) string {
	if r, ok := r.(interface{ ReducerLabel() string }); ok {
		if lbl := r.ReducerLabel(); lbl != "" {
			return lbl
		}
	}
	if lbl := r.ReLabel(); lbl != "" {
		return lbl
	}
	if t := reflect.TypeOf(r); t != nil {
		return t.String()
	}
	return "mg.Reducer"
}
