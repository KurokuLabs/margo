package golang

import (
	"github.com/mdempsky/gocode/suggest"
	"go/build"
	"go/types"
	"margo.sh/mg"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

type gsuOpts struct {
	ProposeBuiltins bool
	Debug           bool
}

type gsuImpRes struct {
	pkg *types.Package
	err error
}

type gcSuggest struct {
	gsuOpts
	sync.Mutex
	imp *gsuImporter
}

func newGcSuggest(mx *mg.Ctx, o gsuOpts) *gcSuggest {
	gsu := &gcSuggest{gsuOpts: o}
	gsu.imp = gsu.newGsuImporter(mx)
	return gsu
}

func (gsu *gcSuggest) newGsuImporter(mx *mg.Ctx) *gsuImporter {
	gi := &gsuImporter{
		mx:  mx,
		bld: BuildContext(mx),
		gsu: gsu,
		res: map[mgcCacheKey]gsuImpRes{},
	}
	return gi
}

func (gsu *gcSuggest) candidates(mx *mg.Ctx) []suggest.Candidate {
	defer mx.Profile.Push("candidates").Pop()
	gsu.Lock()
	defer gsu.Unlock()

	defer func() {
		if e := recover(); e != nil {
			mx.Log.Printf("gocode/suggest panic: %s\n%s\n", e, debug.Stack())
		}
	}()

	cfg := suggest.Config{
		// we no longer support contextual build env :(
		// GoSublime works around this for other packages by restarting the agent
		// if GOPATH changes, so we should be ok
		Importer:   gsu.imp,
		Builtin:    gsu.ProposeBuiltins,
		IgnoreCase: true,
	}
	if gsu.Debug {
		cfg.Logf = func(f string, a ...interface{}) {
			f = "Gocode: " + f
			if !strings.HasSuffix(f, "\n") {
				f += "\n"
			}
			mx.Log.Dbg.Printf(f, a...)
		}
	}

	v := mx.View
	src, _ := v.ReadAll()
	if len(src) == 0 {
		return nil
	}

	l, _ := cfg.Suggest(v.Filename(), src, v.Pos)
	return l
}

type gsuPkgInfo struct {
	// the import path
	Path string

	// the abs path to the package directory
	Dir string

	// the key used for caching
	Key mgcCacheKey

	// whether or not this is a stdlib package
	Std bool
}

type gsuImporter struct {
	mx  *mg.Ctx
	bld *build.Context
	gsu *gcSuggest
	res map[mgcCacheKey]gsuImpRes
}

func (gi *gsuImporter) Import(path string) (*types.Package, error) {
	return gi.ImportFrom(path, ".", 0)
}

func (gi *gsuImporter) ImportFrom(impPath, srcDir string, mode types.ImportMode) (*types.Package, error) {

	// TODO: add mode to the key somehow?
	// mode is reserved, but currently not used so it's not a problem
	// but if it's used in the future, the importer result could depend on it
	//
	// adding it to the key might complicate the pkginfo api because it's called
	// by code that doesn't know anything about mode
	pkgInf, err := mctl.pkgInfo(gi.mx, mctl.srcMode(), impPath, srcDir)
	if err != nil {
		mctl.dbgf("pkgInfo(%q, %q): %s\n", impPath, srcDir, err)
		return nil, err
	}

	// we cache the results of the underlying importer for this *session*
	// because if it fails, we could potentialy end up in a loop
	// trying to import the package again.
	if res, ok := gi.res[pkgInf.Key]; ok {
		return res.pkg, res.err
	}

	// I think we need to use a new underlying importer every time
	// because they cache imports which might depend on srcDir
	//
	// they also have a fileset which could possibly grow indefinitely.
	// I assume using different filesets is ok since we don't make use of it directly
	//
	// at least for the srcImporter, we pass in our own importer as the overlay
	// so we should still get some caching
	//
	// binary imports should hopefully still be fast enough
	pkg, err := gi.importFrom(mctl.defaultImporter(gi.mx, gi), pkgInf, mode)
	if err != nil || !pkg.Complete() {
		pkg, err = gi.importFromFallback(pkgInf, mode, pkg, err)
	}
	switch {
	case err != nil:
		mctl.dbgf("importFrom(%q, %q): %s\n", pkgInf.Path, pkgInf.Dir, err)
	case !pkg.Complete():
		mctl.dbgf("importFrom(%q, %q): pkg is incomplete\n", pkgInf.Path, pkgInf.Dir)
	}

	gi.res[pkgInf.Key] = gsuImpRes{pkg: pkg, err: err}
	return pkg, err
}

func (gi *gsuImporter) importFromFallback(pkgInf gsuPkgInfo, mode types.ImportMode, pkg *types.Package, err error) (*types.Package, error) {
	complete := false
	if pkg != nil {
		complete = pkg.Complete()
	}
	mctl.dbgf("importFrom(%q, %q): fallback: complete=%v, err=%s\n", pkgInf.Path, pkgInf.Dir, complete, err)

	// problem1:
	// if the pkg import fails we will offer no completion
	//
	// problem 2:
	// if it succeeds, but is incomplete we offer completion with `invalid-type` failures
	// i.e. completion stops working at random points for no obvious reason
	//
	// assumption:
	//   it's better to risk using stale data (bin imports)
	//   as opposed to offering no completion at all
	//
	// risks:
	// we will end up caching the result, but that shouldn't be a big deal
	// because if the pkg is edited, thus (possibly) making it importable,
	// we will remove it from the cache anyway.
	// there is the issue about mixing binary (potentially incomplete) pkgs with src pkgs
	// but we were already not going to return anything, so it *shouldn't* apply here

	underlying := mctl.fallbackImporter(gi.mx, gi)
	if underlying == nil {
		return pkg, err
	}

	// import failed, try again
	if err != nil {
		mctl.dbgf("importFrom(%q, %q) failed, trying %T importer\n", pkgInf.Path, pkgInf.Dir, underlying)
		return gi.importFrom(underlying, pkgInf, mode)
	}

	// pkg was imported without error, but it's incomplete
	// it's probably a pkg with `import C`
	mctl.dbgf("importFrom(%q, %q): pkg is incomplete, trying %T importer\n", pkgInf.Path, pkgInf.Dir, underlying)
	p, e := gi.importFrom(underlying, pkgInf, mode)
	if e == nil && p.Complete() {
		return p, e
	}

	return pkg, err
}

func (gi *gsuImporter) importFrom(underlying types.ImporterFrom, pkgInf gsuPkgInfo, mode types.ImportMode) (*types.Package, error) {
	mx := gi.mx

	defer mx.Profile.Push("gsuImport: " + pkgInf.Path).Pop()

	if pkgInf.Std && pkgInf.Path == "unsafe" {
		return types.Unsafe, nil
	}

	if res, ok := gi.res[pkgInf.Key]; ok {
		return res.pkg, res.err
	}

	if e, ok := mctl.pkgs.get(pkgInf.Key); ok {
		return e.Pkg, nil
	}

	impStart := time.Now()
	typPkg, err := underlying.ImportFrom(pkgInf.Path, pkgInf.Dir, mode)
	impDur := time.Since(impStart)

	if err == nil {
		mctl.pkgs.put(mgcCacheEnt{Key: pkgInf.Key, Pkg: typPkg, Dur: impDur})
	} else {
		mctl.dbgf("%T.ImportFrom(%q, %q): %s\n", underlying, pkgInf.Path, pkgInf.Dir, err)
	}

	return typPkg, err
}
