package kimporter

import (
	"encoding/hex"
	"fmt"
	"go/build"
	"go/token"
	"go/types"
	"golang.org/x/crypto/blake2b"
	"margo.sh/golang/gopkg"
	"margo.sh/golang/goutil"
	"margo.sh/mg"
	"margo.sh/mgutil"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	sharedCache = &stateCache{m: map[stateKey]*state{}}
)

type stateKey struct {
	ImportPath   string
	Dir          string
	CheckFuncs   bool
	CheckImports bool
	ImportC      bool
	Tests        bool
	Tags         string
	GOARCH       string
	GOOS         string
	GOROOT       string
	GOPATH       string
	SrcMapHash   string
}

type stateCache struct {
	mu sync.RWMutex
	m  map[stateKey]*state
}

func (sc *stateCache) state(mx *mg.Ctx, k stateKey) *state {
	// TODO: support vfs invalidation.
	// we can't (currently) make use of .Memo because it deletes the data
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if v, ok := sc.m[k]; ok {
		return v
	}
	v := &state{ImportPath: k.ImportPath}
	sc.m[k] = v
	return v
}

type state struct {
	ImportPath string

	mu      sync.Mutex
	err     error
	hardErr error
	pkg     *types.Package
	checked bool
}

func (st *state) result() (*types.Package, error) {
	switch {
	case !st.checked:
		return nil, fmt.Errorf("import cycle via %s", st.ImportPath)
	case st.hardErr != nil:
		return nil, st.hardErr
	case st.err != nil:
		return st.pkg, st.err
	case !st.pkg.Complete():
		// Package exists but is not complete - we cannot handle this
		// at the moment since the source importer replaces the package
		// wholesale rather than augmenting it (see #19337 for details).
		// Return incomplete package with error (see #16088).
		return st.pkg, fmt.Errorf("reimported partially imported package %q", st.ImportPath)
	default:
		return st.pkg, nil
	}
}

type Importer struct {
	SrcMap        map[string][]byte
	CheckFuncs    bool
	CheckImports  bool
	ImportC       bool
	NoConcurrency bool

	mx  *mg.Ctx
	bld *build.Context
	mp  *gopkg.ModPath
	sc  *stateCache
	par *Importer

	mu       *sync.Mutex
	tags     string
	imported map[stateKey]bool
}

func (kp *Importer) Import(path string) (*types.Package, error) {
	return kp.ImportFrom(path, ".", 0)
}

func (kp *Importer) ImportFrom(ipath, srcDir string, mode types.ImportMode) (*types.Package, error) {
	// TODO: add support for unsaved-files without a package
	if mode != 0 {
		panic("non-zero import mode")
	}
	if p, err := filepath.Abs(srcDir); err == nil {
		srcDir = p
	}
	if !filepath.IsAbs(srcDir) {
		return nil, fmt.Errorf("srcDir is not absolute: %s", srcDir)
	}
	pp, err := kp.mp.FindPkg(kp.mx, ipath, srcDir)
	if err != nil {
		return nil, err
	}
	return kp.importPkg(pp)
}

func (kp *Importer) copy() *Importer {
	kp.mu.Lock()
	defer kp.mu.Unlock()

	x := *kp
	return &x
}

func (kp *Importer) stateKey(pp *gopkg.PkgPath) stateKey {
	smHash := ""
	if len(kp.SrcMap) != 0 {
		fns := make(sort.StringSlice, len(kp.SrcMap))
		for fn, _ := range kp.SrcMap {
			fns = append(fns, fn)
		}
		fns.Sort()
		b2, _ := blake2b.New256(nil)
		for _, fn := range fns {
			b2.Write([]byte(fn))
			b2.Write(kp.SrcMap[fn])
		}
		smHash = hex.EncodeToString(b2.Sum(nil))
	}

	// user settings don't apply when checking deps
	userSettings := kp.par == nil
	return stateKey{
		ImportPath:   pp.ImportPath,
		Dir:          pp.Dir,
		CheckFuncs:   userSettings && kp.CheckFuncs,
		CheckImports: userSettings && kp.CheckImports,
		ImportC:      userSettings && kp.ImportC,
		Tests:        strings.HasSuffix(kp.mx.View.Name, "_test.go"),
		Tags:         kp.tags,
		GOOS:         kp.bld.GOOS,
		GOARCH:       kp.bld.GOARCH,
		GOROOT:       kp.bld.GOROOT,
		SrcMapHash:   smHash,
		GOPATH:       strings.Join(mgutil.PathList(kp.bld.GOPATH), string(filepath.ListSeparator)),
	}
}

func (kp *Importer) importPkg(pp *gopkg.PkgPath) (*types.Package, error) {
	title := "Kim-Porter: import(" + pp.Dir + ")"
	defer kp.mx.Profile.Push(title).Pop()
	defer kp.mx.Begin(mg.Task{Title: title}).Done()

	sk := kp.stateKey(pp)
	st := kp.sc.state(kp.mx, sk)
	kp.mu.Lock()
	imported, importing := kp.imported[sk]
	if !importing {
		kp.imported[sk] = false
	}
	kp.mu.Unlock()
	if imported {
		return st.result()
	}
	defer func() {
		kp.mu.Lock()
		kp.imported[sk] = true
		kp.mu.Unlock()
	}()
	return kp.check(sk, st, pp)
}

func (kp *Importer) check(sk stateKey, st *state, pp *gopkg.PkgPath) (*types.Package, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.checked {
		return st.result()
	}

	if pp.Goroot && pp.ImportPath == "unsafe" {
		st.checked = true
		st.pkg = types.Unsafe
		return st.result()
	}

	kx := kp.branch(pp)
	fset := token.NewFileSet()
	bp, _, astFiles, err := parseDir(kx.mx, kx.bld, fset, pp.Dir, kx.SrcMap, sk)
	if err != nil {
		return nil, err
	}

	// we might as well reace ahead and load the imports concurrently
	kx.preloadImports(sk, bp)
	tc := types.Config{
		FakeImportC:              !sk.ImportC,
		IgnoreFuncBodies:         !sk.CheckFuncs,
		DisableUnusedImportCheck: !sk.CheckImports,
		Error: func(err error) {
			if te, ok := err.(types.Error); ok && !te.Soft && st.hardErr == nil {
				st.hardErr = err
			}
		},
		Importer: kx,
		Sizes:    types.SizesFor(kx.bld.Compiler, kx.bld.GOARCH),
	}
	st.pkg, st.err = tc.Check(bp.ImportPath, fset, astFiles, nil)
	if st.err == nil && st.hardErr != nil {
		st.err = st.hardErr
	}
	st.checked = true
	return st.result()

}

func (kp *Importer) preloadImports(sk stateKey, bp *build.Package) {
	if kp.NoConcurrency {
		return
	}
	preload := func(ipath string) {
		pp, err := kp.mp.FindPkg(kp.mx, ipath, bp.Dir)
		if err != nil {
			return
		}
		kx := kp.branch(pp)

		sk := kx.stateKey(pp)
		kx.mu.Lock()
		_, importing := kx.imported[sk]
		kx.mu.Unlock()
		if importing {
			return
		}

		go kx.importPkg(pp)
	}
	for _, ipath := range bp.Imports {
		preload(ipath)
	}
	if sk.Tests {
		for _, ipath := range bp.TestImports {
			preload(ipath)
		}
	}
}

func (kp *Importer) setupJs(pp *gopkg.PkgPath) {
	fs := kp.mx.VFS
	nd := fs.Poke(kp.bld.GOROOT).Poke("src/syscall/js")
	if fs.Poke(pp.Dir) != nd && fs.Poke(kp.mx.View.Dir()) != nd {
		return
	}
	bld := *kp.bld
	bld.GOOS = "js"
	bld.GOARCH = "wasm"
	kp.bld = &bld
}

func (kp *Importer) branch(pp *gopkg.PkgPath) *Importer {
	kx := kp.copy()
	if pp.Mod != nil {
		kx.mp = pp.Mod
	}
	kx.par = kp
	kx.setupJs(pp)
	return kx
}

func New(mx *mg.Ctx) *Importer {
	bld := goutil.BuildContext(mx)
	return &Importer{
		mx:  mx,
		bld: bld,
		sc:  sharedCache,

		mu:       &sync.Mutex{},
		imported: map[stateKey]bool{},
		tags:     tagsStr(bld.BuildTags),
	}
}

func tagsStr(l []string) string {
	switch len(l) {
	case 0:
		return ""
	case 1:
		return l[0]
	}
	s := append(sort.StringSlice{}, l...)
	s.Sort()
	return strings.Join(s, " ")
}
