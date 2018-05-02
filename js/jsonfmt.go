package js

import (
	"encoding/json"
	"margo.sh/mg"
)

type JsonFmt struct {
	mg.ReducerType

	Prefix string
	Indent string
}

func (j JsonFmt) Reduce(mx *mg.Ctx) *mg.State {
	if !mx.View.LangIs("json") {
		return mx.State
	}
	if _, ok := mx.Action.(mg.ViewFmt); !ok {
		return mx.State
	}

	fn := mx.View.Filename()
	r, err := mx.View.Open()
	if err != nil {
		return mx.AddErrorf("failed to open %s: %s\n", fn, err)
	}
	defer r.Close()

	var v interface{}
	if err := json.NewDecoder(r).Decode(&v); err != nil {
		return mx.AddErrorf("failed to unmarshal json %s: %s\n", fn, err)
	}

	src, err := json.MarshalIndent(v, j.Prefix, j.Indent)
	if err != nil {
		return mx.AddErrorf("failed to marshal json %s: %s\n", fn, err)
	}
	return mx.SetSrc(src)
}
