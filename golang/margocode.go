package golang

import (
	"bytes"
	"fmt"
	"go/build"
	"go/types"
	"margo.sh/mg"
	"margo.sh/mgpf"
	"math"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
)

var (
	mctl = &marGocodeCtl{}
)

type marGocodeCtl struct {
	pkgs *mgcCache

	initMu sync.RWMutex
	inited bool
	confed bool
	cmdMap map[string]func(*mg.CmdCtx)

	dbgMu sync.RWMutex
	dbgF  func(format string, a ...interface{})
	dbgP  func(*types.Package) bool
}

func (mgc *marGocodeCtl) subscriber(mx *mg.Ctx) {
	switch mx.Action.(type) {
	case mg.ViewModified, mg.ViewSaved:
		// ViewSaved is probably not required, but saving might result in a `go install`
		// which results in an updated package.a file
		mgc.autoPruneCache(mx)
	}
}

func (mgc *marGocodeCtl) autoPruneCache(mx *mg.Ctx) {
	// TODO: do this in a goroutine?
	// we're not directly in the QueryCompletions hot path
	// but we *are* in a subscriber, so we're blocking the store
	// if something like pkginfo or DebugPrune is slow,
	// we will end up blocking the next reduction

	for _, source := range []bool{true, false} {
		if pkgInf, err := mgc.pkgInfo(mx, source, ".", mx.View.Dir()); err == nil {
			mgc.pkgs.del(pkgInf.Key)
		}
	}

	mgc.dbgMu.RLock()
	defer mgc.dbgMu.RUnlock()

	if dpr := mgc.dbgP; dpr != nil {
		mgc.pkgs.forEach(func(e mgcCacheEnt) {
			if dpr(e.Pkg) {
				mgc.pkgs.del(e.Key)
			}
		})
	}
}

func (mgc *marGocodeCtl) confOnce(mx *mg.Ctx, ctl *MarGocodeCtl) {
	mgc.initMu.Lock()
	defer mgc.initMu.Unlock()

	if mgc.confed {
		return
	}
	mgc.confed = true

	if ctl.Debug {
		mgc.dbgMu.Lock()
		mgc.dbgF = mx.Log.Dbg.Printf
		mgc.dbgMu.Unlock()
	}
	mgc.dbgP = ctl.DebugPrune
}

func (mgc *marGocodeCtl) initOnce(mx *mg.Ctx) {
	mgc.initMu.Lock()
	defer mgc.initMu.Unlock()

	if mgc.inited {
		return
	}
	mgc.inited = true

	go mx.Store.Subscribe(mgc.subscriber)

	mgc.pkgs = &mgcCache{m: map[mgcCacheKey]mgcCacheEnt{}}
	mgc.cmdMap = map[string]func(*mg.CmdCtx){
		"help": mgc.helpCmd,
		"cache-list-by-key": func(cx *mg.CmdCtx) {
			mgc.listCacheCmd(cx, func(ents []mgcCacheEnt, i, j int) bool {
				return ents[i].Key < ents[j].Key
			})
		},
		"cache-list-by-dur": func(cx *mg.CmdCtx) {
			mgc.listCacheCmd(cx, func(ents []mgcCacheEnt, i, j int) bool {
				return ents[i].Dur < ents[j].Dur
			})
		},
		"cache-prune": mgc.pruneCacheCmd,
	}
}

func (mgc *marGocodeCtl) pkgInfo(mx *mg.Ctx, source bool, impPath, srcDir string) (gsuPkgInfo, error) {
	// TODO: cache these ops?
	// it might not be worth the added complexity since we will get a lot of impPath=io
	// with a different srcPath which means we have to look it up anyway.
	//
	// TODO: support go modules
	// at this time, go/packages appears to be extremely slow
	// it takes 100ms+ just to load the errors packages in LoadFiles mode
	//
	// in eiter case, in go1.11 we might end up calling `go list` which is very slow

	bpkg, err := BuildContext(mx).Import(impPath, srcDir, build.FindOnly)
	if err != nil {
		return gsuPkgInfo{}, err
	}
	return gsuPkgInfo{
		Path: bpkg.ImportPath,
		Dir:  bpkg.Dir,
		Key:  mkMgcCacheKey(source, bpkg.Dir),
		Std:  bpkg.Goroot,
	}, nil
}

func (mgc *marGocodeCtl) dbgf(format string, a ...interface{}) {
	mgc.dbgMu.Lock()
	defer mgc.dbgMu.Unlock()

	if mgc.dbgF == nil {
		return
	}

	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}
	mgc.dbgF("margocode: "+format, a...)
}

func (mgc *marGocodeCtl) cmds() mg.BuiltinCmdList {
	return mg.BuiltinCmdList{
		mg.BuiltinCmd{
			Name: "margocodectl",
			Desc: "introspect and manage the margocode cache and state",
			Run:  mgc.cmd,
		},
	}
}

func (mgc *marGocodeCtl) listCacheCmd(cx *mg.CmdCtx, less func(ents []mgcCacheEnt, i, j int) bool) {
	defer cx.Output.Close()

	ents := mctl.pkgs.entries()
	if len(ents) == 0 {
		fmt.Fprintln(cx.Output, "The cache is empty")
		return
	}

	buf := &bytes.Buffer{}
	tbw := tabwriter.NewWriter(cx.Output, 1, 4, 1, ' ', 0)
	defer tbw.Flush()

	digits := int(math.Floor(math.Log10(float64(len(ents)))) + 1)
	sfxFormat := "\t%s\t%s\n"
	hdrFormat := "%s" + sfxFormat
	rowFormat := fmt.Sprintf("%%%dd/%d", digits, len(ents)) + sfxFormat

	sort.Slice(ents, func(i, j int) bool { return less(ents, i, j) })
	fmt.Fprintf(buf, hdrFormat, "Count:", "Package Key:", "Import Duration:")
	for i, e := range ents {
		fmt.Fprintf(buf, rowFormat, i+1, e.Key, mgpf.D(e.Dur))
	}
	tbw.Write(buf.Bytes())
}

func (mgc *marGocodeCtl) pruneCacheCmd(cx *mg.CmdCtx) {
	defer cx.Output.Close()

	args := cx.Args
	if len(args) == 0 {
		args = []string{".*"}
	}

	pats := make([]*regexp.Regexp, 0, len(args))
	for _, s := range args {
		p, err := regexp.Compile(s)
		if err == nil {
			pats = append(pats, p)
		} else {
			fmt.Fprintf(cx.Output, "Error: regexp.Compile(%s): %s\n", s, err)
		}
	}

	ents := mctl.pkgs.prune(pats...)
	for _, e := range ents {
		fmt.Fprintln(cx.Output, "Pruned:", e.Key)
	}
	fmt.Fprintln(cx.Output, "Pruned", len(ents), "entries")
	debug.FreeOSMemory()
}

func (mgc *marGocodeCtl) helpCmd(cx *mg.CmdCtx) {
	defer cx.Output.Close()

	cx.Output.Write([]byte(`Usage: margocodectl $subcmd [args...]
	cache-prune [regexp, or path...] - remove packages matching glob from the cache. default: '.*'
	cache-list-by-key                - list cached packages, sorted by key
	cache-list-by-dur                - list cached packages, sorted by import duration
`))
}

func (mgc *marGocodeCtl) cmd(cx *mg.CmdCtx) *mg.State {
	cmd := mgc.helpCmd
	if len(cx.Args) > 0 {
		sub := cx.Args[0]
		if c, ok := mgc.cmdMap[sub]; ok {
			cmd = c
			cx = cx.Copy(func(cx *mg.CmdCtx) {
				cx.Args = cx.Args[1:]
			})
		} else {
			fmt.Fprintln(cx.Output, "Unknown subcommand:", sub)
		}
	}
	go cmd(cx)
	return cx.State
}

type MarGocodeCtl struct {
	mg.ReducerType

	// Whether or not to print debugging info related to the gocode cache
	// used by the Gocode and GocodeCalltips reducers
	Debug bool

	// DebugPrune returns true if pkg should be removed from the cache
	DebugPrune func(pkg *types.Package) bool
}

func (mgc *MarGocodeCtl) ReducerMount(mx *mg.Ctx) {
	mctl.initOnce(mx)
	mctl.confOnce(mx, mgc)
}

func (mgc *MarGocodeCtl) Reduce(mx *mg.Ctx) *mg.State {
	switch mx.Action.(type) {
	case mg.RunCmd:
		return mx.AddBuiltinCmds(mctl.cmds()...)
	default:
		return mx.State
	}
}
