package golang

import (
	"disposa.blue/margo/mg"
	"go/ast"
	"go/token"
)

const (
	PackageScope CompletionScope = 1 << iota
	FileScope
	DeclScope
	BlockScope
	ImportScope
	ConstScope
	VarScope
	TypeScope
	CommentScope
	StringScope
	ImportPathScope
)

type CompletionScope uint64

func (cs CompletionScope) Any(scopes ...CompletionScope) bool {
	for _, s := range scopes {
		if cs&s != 0 {
			return true
		}
	}
	return false
}

func (cs CompletionScope) All(scopes ...CompletionScope) bool {
	for _, s := range scopes {
		if cs&s == 0 {
			return false
		}
	}
	return true
}

type CompletionCtx struct {
	*mg.Ctx
	CursorNode *CursorNode
	AstFile    *ast.File
	Scope      CompletionScope
	PkgName    string
}

func NewCompletionCtx(mx *mg.Ctx, src []byte, pos int) *CompletionCtx {
	cn := ParseCursorNode(mx.Store, src, pos)
	af := cn.AstFile
	if af == nil {
		af = NilAstFile
	}
	cx := &CompletionCtx{
		Ctx:        mx,
		CursorNode: cn,
		AstFile:    af,
		PkgName:    af.Name.String(),
	}
	switch {
	case cx.PkgName == "":
		cx.Scope |= PackageScope
	case cn.BlockStmt == nil && cn.GenDecl == nil:
		cx.Scope |= FileScope
	case cn.BlockStmt != nil:
		cx.Scope |= BlockScope
	}
	if gd := cn.GenDecl; gd != nil {
		switch gd.Tok {
		case token.IMPORT:
			cx.Scope |= ImportScope
		case token.CONST:
			cx.Scope |= ConstScope
		case token.VAR:
			cx.Scope |= VarScope
		case token.TYPE:
			cx.Scope |= TypeScope
		}
	}
	if cn.Comment != nil {
		cx.Scope |= CommentScope
	}
	if lit := cn.BasicLit; lit != nil && lit.Kind == token.STRING {
		if cn.ImportSpec != nil {
			cx.Scope |= ImportPathScope
		} else {
			cx.Scope |= StringScope
		}
	}
	return cx
}
