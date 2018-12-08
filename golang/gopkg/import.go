package gopkg

import (
	"margo.sh/golang/goutil"
	"margo.sh/mg"
	"margo.sh/vfs"
	"path/filepath"
)

func ImportDir(mx *mg.Ctx, path string) (*Pkg, error) {
	path = filepath.Clean(path)

	kv, err := vfs.Root.KV(path)
	if err != nil {
		return nil, err
	}

	type K struct{ path string }
	k := K{path}
	if p, ok := kv.Get(k).(*Pkg); ok {
		return p, nil
	}

	bpkg, err := goutil.BuildContext(mx).ImportDir(path, 0)
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
	kv.Put(k, p)
	return p, nil
}
