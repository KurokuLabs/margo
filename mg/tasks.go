package mg

import (
	"bytes"
	"fmt"
	"sync"
	"time"
)

type taskTick struct{ ActionType }

type Task struct {
	Title    string
	Cancel   func()
	CancelID string
	ShowNow  bool
}

type TaskTicket struct {
	ID       string
	Title    string
	Start    time.Time
	CancelID string

	tracker *taskTracker
	showNow bool
	cancel  func()
}

func (ti *TaskTicket) Done() {
	if ti.tracker != nil {
		ti.tracker.done(ti.ID)
	}
}

func (ti *TaskTicket) Cancel() {
	if ti.cancel != nil {
		ti.cancel()
	}
}

func (ti *TaskTicket) Cancellable() bool {
	return ti.cancel != nil
}

type taskTracker struct {
	ReducerType
	mu      sync.Mutex
	id      uint64
	tickets []*TaskTicket
	timer   *time.Timer
	buf     bytes.Buffer
}

func (tr *taskTracker) ReducerMount(mx *Ctx) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	tr.timer = time.NewTimer(1 * time.Second)
	dispatch := mx.Store.Dispatch
	go func() {
		for range tr.timer.C {
			dispatch(taskTick{})
		}
	}()
}

func (tr *taskTracker) ReducerUnmount(*Ctx) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	for _, t := range tr.tickets {
		t.Cancel()
	}
}

func (tr *taskTracker) Reduce(mx *Ctx) *State {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	st := mx.State
	switch mx.Action.(type) {
	case RunCmd:
		st = tr.runCmd(st)
	case QueryUserCmds:
		st = tr.userCmds(st)
	case taskTick:
		tr.tick()
	}
	if s := tr.status(); s != "" {
		st = st.AddStatus(s)
	}
	return st
}

func (tr *taskTracker) tick() {
	if len(tr.tickets) != 0 {
		tr.resetTimer()
	}
}

func (tr *taskTracker) userCmds(st *State) *State {
	cl := make([]UserCmd, len(tr.tickets))
	for i, t := range tr.tickets {
		c := UserCmd{
			Title: "Cancel " + t.Title,
			Name:  ".kill",
		}
		for _, s := range []string{t.CancelID, t.ID} {
			if s != "" {
				c.Args = append(c.Args, s)
			}
		}
		cl[i] = c
	}
	return st.AddUserCmds(cl...)
}

func (tr *taskTracker) runCmd(st *State) *State {
	return st.AddBuiltinCmds(
		BultinCmd{
			Name: ".kill",
			Desc: "List and cancel active tasks",
			Run:  tr.killBuiltin,
		},
	)
}

// Cancel cancels the task tid.
// true is returned if the task exists and was canceled
func (tr *taskTracker) Cancel(tid string) bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	return tr.cancel(tid)
}

func (tr *taskTracker) cancel(tid string) bool {
	for _, t := range tr.tickets {
		if t.ID == tid || t.CancelID == tid {
			t.Cancel()
			return t.Cancellable()
		}
	}
	return false
}

func (tr *taskTracker) killBuiltin(cx *CmdCtx) *State {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	defer cx.Output.Close()
	if len(cx.Args) == 0 {
		tr.listAll(cx)
	} else {
		tr.killAll(cx)
	}

	return cx.State
}

func (tr *taskTracker) killAll(cx *CmdCtx) {
	buf := &bytes.Buffer{}
	for _, tid := range cx.Args {
		fmt.Fprintf(buf, "%s: %v\n", tid, tr.cancel(tid))
	}
	cx.Output.Write(buf.Bytes())
}

func (tr *taskTracker) listAll(cx *CmdCtx) {
	buf := &bytes.Buffer{}
	for _, t := range tr.tickets {
		id := t.ID
		if t.CancelID != "" {
			id += "|" + t.CancelID
		}

		dur := time.Since(t.Start)
		if dur < time.Second {
			dur = dur.Round(time.Millisecond)
		} else {
			dur = dur.Round(time.Second)
		}

		fmt.Fprintf(buf, "ID: %s, Dur: %s, Title: %s\n", id, dur, t.Title)
	}
	cx.Output.Write(buf.Bytes())
}

func (tr *taskTracker) status() string {
	tr.buf.Reset()
	now := time.Now()
	tr.buf.WriteString("Tasks")
	initLen := tr.buf.Len()
	title := ""
	for _, t := range tr.tickets {
		age := now.Sub(t.Start) / time.Second
		switch age {
		case 0:
		case 1:
			tr.buf.WriteString(" ◔")
		case 2:
			tr.buf.WriteString(" ◑")
		case 3:
			tr.buf.WriteString(" ◕")
		default:
			tr.buf.WriteString(" ●")
		}
		if title == "" && t.Title != "" && (age >= 1 || t.showNow) && age <= 3 {
			title = t.Title
		}
	}
	if tr.buf.Len() == initLen && title == "" {
		return ""
	}
	if title != "" {
		tr.buf.WriteByte(' ')
		tr.buf.WriteString(title)
	}
	return tr.buf.String()
}

func (tr *taskTracker) titles() (stale []string, fresh []string) {
	now := time.Now()
	for _, t := range tr.tickets {
		dur := now.Sub(t.Start)
		switch {
		case dur >= 5*time.Second:
			stale = append(stale, t.Title)
		case dur >= 1*time.Second:
			fresh = append(fresh, t.Title)
		}
	}
	for _, t := range tr.tickets {
		dur := now.Sub(t.Start)
		switch {
		case dur >= 5*time.Second:
			stale = append(stale, t.Title)
		case dur >= 1*time.Second:
			fresh = append(fresh, t.Title)
		}
	}
	return stale, fresh
}

func (tr *taskTracker) resetTimer() {
	defer tr.timer.Reset(1 * time.Second)
}

func (tr *taskTracker) done(id string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	defer tr.resetTimer()

	l := make([]*TaskTicket, 0, len(tr.tickets)-1)
	for _, t := range tr.tickets {
		if t.ID != id {
			l = append(l, t)
		}
	}
	tr.tickets = l
}

func (tr *taskTracker) Begin(o Task) *TaskTicket {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	defer tr.resetTimer()

	if cid := o.CancelID; cid != "" {
		for _, t := range tr.tickets {
			if t.CancelID == cid {
				t.Cancel()
			}
		}
	}

	tr.id++
	t := &TaskTicket{
		ID:       fmt.Sprintf("@%d", tr.id),
		CancelID: o.CancelID,
		Title:    o.Title,
		Start:    time.Now(),
		cancel:   o.Cancel,
		tracker:  tr,
		showNow:  o.ShowNow,
	}
	tr.tickets = append(tr.tickets, t)
	return t
}
