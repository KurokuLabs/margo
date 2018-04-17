package mg

import (
	"context"
	"fmt"
	"go/build"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/ugorji/go/codec"
	"margo.sh/misc/pprof/pprofdo"
)

var (
	ErrNoSettings = fmt.Errorf("no editor settings")

	_ context.Context = (*Ctx)(nil)
)

type Ctx struct {
	*State
	Action Action

	Store *Store

	Log *Logger

	Parent *Ctx
	Values map[interface{}]interface{}
	DoneC  <-chan struct{}

	handle codec.Handle
}

func (*Ctx) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (mx *Ctx) Done() <-chan struct{} {
	return mx.DoneC
}

func (*Ctx) Err() error {
	return nil
}

func (mx *Ctx) Value(k interface{}) interface{} {
	if v, ok := mx.Values[k]; ok {
		return v
	}
	if mx.Parent != nil {
		return mx.Parent.Value(k)
	}
	return nil
}

func (mx *Ctx) AgentName() string {
	return mx.Store.ag.Name
}

func newCtx(ag *Agent, st *State, act Action, sto *Store) (mx *Ctx, done chan struct{}) {
	if st == nil {
		panic("newCtx: state must not be nil")
	}
	if st == nil {
		panic("newCtx: store must not be nil")
	}
	done = make(chan struct{})
	return &Ctx{
		State:  st,
		Action: act,

		Store: sto,

		Log: ag.Log,

		DoneC: done,

		handle: ag.handle,
	}, done
}

func (mx *Ctx) ActionIs(actions ...Action) bool {
	typ := reflect.TypeOf(mx.Action)
	for _, act := range actions {
		if reflect.TypeOf(act) == typ {
			return true
		}
	}
	return false
}

func (mx *Ctx) LangIs(names ...string) bool {
	return mx.View.LangIs(names...)
}

func (mx *Ctx) Copy(updaters ...func(*Ctx)) *Ctx {
	x := *mx
	x.Parent = mx
	if len(mx.Values) != 0 {
		x.Values = make(map[interface{}]interface{}, len(mx.Values))
		for k, v := range mx.Values {
			x.Values[k] = v
		}
	}
	for _, f := range updaters {
		f(&x)
	}
	return &x
}

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

type stickyState struct {
	View   *View
	Env    EnvMap
	Editor EditorProps
}

type State struct {
	stickyState
	Config        EditorConfig
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
		stickyState: stickyState{View: newView(sto)},
	}
}

func (st *State) new() *State {
	return &State{stickyState: st.stickyState}
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
