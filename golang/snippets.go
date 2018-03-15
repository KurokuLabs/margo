package golang

import (
	"disposa.blue/margo/mg"
	"go/ast"
	"strings"
)

var (
	Snippets = SnippetFuncs{
		PackageNameSnippet,
		MainFuncSnippet,
		InitFuncSnippet,
		FuncSnippet,
		GenDeclSnippet,
		MapSnippet,
	}
)

type SnippetFuncs []func(*CompletionCtx) []mg.Completion

func (sf SnippetFuncs) Reduce(mx *mg.Ctx) *mg.State {
	if !mx.LangIs("go") || !mx.ActionIs(mg.QueryCompletions{}) {
		return mx.State
	}

	src, _ := mx.View.ReadAll()
	cx := NewCompletionCtx(mx, src, mx.View.Pos)
	if cx.Scope.Any(StringScope, ImportPathScope, CommentScope) {
		return mx.State
	}

	var cl []mg.Completion
	for _, f := range sf {
		cl = append(cl, f(cx)...)
	}
	for i, _ := range cl {
		sf.fixCompletion(&cl[i])
	}
	return mx.State.AddCompletions(cl...)
}

func (sf SnippetFuncs) fixCompletion(c *mg.Completion) {
	if c.Tag == "" {
		c.Tag = mg.SnippetTag
	}
}

func PackageNameSnippet(cx *CompletionCtx) []mg.Completion {
	if cx.PkgName != "" || !cx.Scope.Is(PackageScope) {
		return nil
	}

	name := "main"
	bx := BuildContext(cx.Ctx)
	pkg, _ := bx.ImportDir(cx.View.Dir(), 0)
	if pkg != nil && pkg.Name != "" {
		name = pkg.Name
	}

	return []mg.Completion{{
		Query: `package ` + name,
		Src: strings.TrimSpace(`
package ` + name + `

$0
		`),
	}}
}

func MainFuncSnippet(cx *CompletionCtx) []mg.Completion {
	if !cx.Scope.Is(FileScope) || cx.PkgName != "main" {
		return nil
	}

	for _, x := range cx.AstFile.Decls {
		x, ok := x.(*ast.FuncDecl)
		if ok && x.Name != nil && x.Name.String() == "main" {
			return nil
		}
	}

	return []mg.Completion{{
		Query: `func main`,
		Title: `main() {...}`,
		Src: strings.TrimSpace(`
func main() {
	$0
}
		`),
	}}
}

func InitFuncSnippet(cx *CompletionCtx) []mg.Completion {
	if !cx.Scope.Is(FileScope) {
		return nil
	}

	for _, x := range cx.AstFile.Decls {
		x, ok := x.(*ast.FuncDecl)
		if ok && x.Name != nil && x.Name.String() == "init" {
			return nil
		}
	}

	return []mg.Completion{{
		Query: `func init`,
		Title: `init() {...}`,
		Src: strings.TrimSpace(`
func init() {
	$0
}
		`),
	}}
}

func FuncSnippet(cx *CompletionCtx) []mg.Completion {
	if cx.Scope.Is(FileScope) {
		return []mg.Completion{{
			Query: `func`,
			Title: `name() {...}`,
			Src: strings.TrimSpace(`
func ${1:name}($2)$3 {
	$0
}
			`),
		}}
	}

	if cx.Scope.Any(BlockScope, VarScope) {
		return []mg.Completion{{
			Query: `func`,
			Title: `func() {...}`,
			Src: strings.TrimSpace(`
func($1)$2 {
	$3
}$0
			`),
		}}
	}

	return nil
}

func GenDeclSnippet(cx *CompletionCtx) []mg.Completion {
	if !cx.Scope.Is(FileScope) {
		return nil
	}
	return []mg.Completion{
		{
			Query: `import`,
			Title: `(...)`,
			Src: strings.TrimSpace(`
import (
	"$0"
)
			`),
		},
		{
			Query: `var`,
			Title: `(...)`,
			Src: strings.TrimSpace(`
var (
	${1:name} = ${2:value}
)
			`),
		},
		{
			Query: `const`,
			Title: `(...)`,
			Src: strings.TrimSpace(`
const (
	${1:name} = ${2:value}
)
			`),
		},
	}
}

func MapSnippet(cx *CompletionCtx) []mg.Completion {
	if !cx.Scope.Any(VarScope, BlockScope) {
		return nil
	}
	return []mg.Completion{
		{
			Query: `map`,
			Title: `map[T]T`,
			Src:   `map[${1:T}]${2:T}`,
		},
		{
			Query: `map`,
			Title: `map[T]T{...}`,
			Src:   `map[${1:T}]${2:T}{$0}`,
		},
	}
}
