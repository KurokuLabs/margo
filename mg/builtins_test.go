package mg_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"margo.sh/mg"
)

func TestBultinCmdList_Lookup(t *testing.T) {
	exec := func() mg.BultinCmd {
		r, _ := mg.Builtins.Commands().Lookup(".exec")
		return r
	}()
	item := mg.BultinCmd{
		Name: "this name",
		Desc: "description",
		Run:  func(*mg.BultinCmdCtx) *mg.State { return nil },
	}
	tcs := []struct {
		name      string
		bcl       mg.BultinCmdList
		input     string
		wantCmd   mg.BultinCmd
		wantFound bool
	}{
		{"empty cmd list", mg.BultinCmdList{}, "nothing to find", exec, false},
		{"not found", mg.BultinCmdList{item}, "not found", exec, false},
		{"found", mg.BultinCmdList{item}, item.Name, item, true},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			gotCmd, gotFound := tc.bcl.Lookup(tc.input)
			// there is no way to compare functions, therefore we just check the names.
			if gotCmd.Name != tc.wantCmd.Name {
				t.Errorf("Lookup(): gotCmd = (%v); want (%v)", gotCmd, tc.wantCmd)
			}
			if gotFound != tc.wantFound {
				t.Errorf("Lookup(): gotFound = (%v); want (%v)", gotFound, tc.wantFound)
			}
		})
	}
}

// tests when the Args is empty, it should pick up the available BuiltinCmd(s).
func TestTypeCmdEmptyArgs(t *testing.T) {
	item1 := mg.BultinCmd{Name: "this name", Desc: "this description"}
	item2 := mg.BultinCmd{Name: "another one", Desc: "should appear too"}
	buf := new(bytes.Buffer)
	input := &mg.BultinCmdCtx{
		Ctx: &mg.Ctx{
			State: &mg.State{
				BuiltinCmds: mg.BultinCmdList{item1, item2},
			},
		},
		Output: &mg.CmdOutputWriter{
			Writer:   buf,
			Dispatch: nil,
		},
	}

	if got := mg.TypeCmd(input); !reflect.DeepEqual(got, input.State) {
		t.Errorf("TypeCmd() = %v, want %v", got, input.State)
	}
	out := buf.String()
	for _, item := range []mg.BultinCmd{item1, item2} {
		if !strings.Contains(out, item.Name) {
			t.Errorf("buf.String() = (%s); want (%s) in it", out, item.Name)
		}
		if !strings.Contains(out, item.Desc) {
			t.Errorf("buf.String() = (%s); want (%s) in it", out, item.Desc)
		}
	}
}

// tests when command is found, it should choose it.
func TestTypeCmdLookupCmd(t *testing.T) {
	item1 := mg.BultinCmd{Name: "this name", Desc: "this description"}
	item2 := mg.BultinCmd{Name: "another one", Desc: "should not appear"}
	buf := new(bytes.Buffer)
	input := &mg.BultinCmdCtx{
		Ctx: &mg.Ctx{
			State: &mg.State{
				BuiltinCmds: mg.BultinCmdList{item1, item2},
			},
		},
		Output: &mg.CmdOutputWriter{
			Writer:   buf,
			Dispatch: nil,
		},
		RunCmd: mg.RunCmd{
			Args: []string{item2.Name},
		},
	}

	if got := mg.TypeCmd(input); !reflect.DeepEqual(got, input.State) {
		t.Errorf("TypeCmd() = %v, want %v", got, input.State)
	}
	out := buf.String()
	if strings.Contains(out, item1.Name) {
		t.Errorf("buf.String() = (%s); didn't expect (%s) in it", out, item1.Name)
	}
	if strings.Contains(out, item1.Name) {
		t.Errorf("buf.String() = (%s); didn't expect (%s) in it", out, item1.Name)
	}
	if !strings.Contains(out, item2.Name) {
		t.Errorf("buf.String() = (%s); want (%s) in it", out, item2.Name)
	}
	if !strings.Contains(out, item2.Name) {
		t.Errorf("buf.String() = (%s); want (%s) in it", out, item2.Name)
	}
}
