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
	}
)

type SnippetFuncs []func(*CompletionCtx) []mg.Completion

func (sf SnippetFuncs) Reduce(mx *mg.Ctx) *mg.State {
	if !mx.LangIs("go") || !mx.ActionIs(mg.QueryCompletions{}) {
		return mx.State
	}

	cx := NewCompletionCtx(mx)
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
	if cx.PkgName != "" {
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
	if !cx.GlobalScope || cx.PkgName != "main" {
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
		Title: `{...}`,
		Src: strings.TrimSpace(`
func main() {
	$0
}
		`),
	}}
}

func InitFuncSnippet(cx *CompletionCtx) []mg.Completion {
	if !cx.GlobalScope {
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
		Title: `{...}`,
		Src: strings.TrimSpace(`
func init() {
	$0
}
		`),
	}}
}

func FuncSnippet(cx *CompletionCtx) []mg.Completion {
	if !cx.GlobalScope {
		return nil
	}
	return []mg.Completion{{
		Query: `func`,
		Title: `{...}`,
		Src: strings.TrimSpace(`
func ${1:name}($2)$3 {
	$0
}
		`),
	}}
}

func GenDeclSnippet(cx *CompletionCtx) []mg.Completion {
	if !cx.GlobalScope {
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
