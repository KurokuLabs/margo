package gopkg

import (
	"go/build"
	"margo.sh/golang/goutil"
	"margo.sh/mg"
	"margo.sh/vfs"
	"path/filepath"
)

func ImportDir(mx *mg.Ctx, dir string) (*Pkg, error) {
	dir = filepath.Clean(dir)
	nd, kv, err := vfs.Root.KV(dir)
	if err != nil {
		return nil, err
	}
	if !nd.SomeSuffix(".go") {
		return nil, &build.NoGoError{Dir: dir}
	}

	bctx := goutil.BuildContext(mx)
	type K struct {
		GOROOT string
		GOPATH string
	}
	k := K{
		GOROOT: bctx.GOROOT,
		GOPATH: bctx.GOPATH,
	}
	v, err := kv.Memo(k, func() (interface{}, error) {
		p, err := importDir(bctx, dir)
		return p, err
	})
	p, _ := v.(*Pkg)
	return p, err
}

func importDir(bctx *build.Context, dir string) (*Pkg, error) {
	bpkg, err := bctx.ImportDir(dir, 0)
	if err != nil {
		return nil, err
	}

	p := &Pkg{
		Dir:        bpkg.Dir,
		Name:       bpkg.Name,
		ImportPath: bpkg.ImportPath,
		Standard:   bpkg.Goroot,
	}
	p.Finalize()
	return p, nil
}
