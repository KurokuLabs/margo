package golang

import (
	"disposa.blue/margo/mg"
	"go/ast"
	"go/token"
)

var (
	NilAstFile = &ast.File{Name: ast.NewIdent("")}
)

type CompletionCtx struct {
	*mg.Ctx
	CursorNode   *CursorNode
	AstFile      *ast.File
	GlobalScope  bool
	LocalScope   bool
	InComment    bool
	InString     bool
	InImportPath bool
	PkgName      string
}

func NewCompletionCtx(mx *mg.Ctx) *CompletionCtx {
	src, _ := mx.View.ReadAll()
	cn := ParseCursorNode(src, mx.View.Pos)
	af := cn.AstFile
	if af == nil {
		af = NilAstFile
	}
	inString := cn.BasicLit != nil && cn.BasicLit.Kind == token.STRING
	pkgname := af.Name.String()
	cx := &CompletionCtx{
		Ctx:          mx,
		CursorNode:   cn,
		AstFile:      af,
		GlobalScope:  pkgname != "" && cn.BlockStmt == nil && cn.GenDecl == nil,
		LocalScope:   cn.BlockStmt != nil,
		InComment:    cn.Comment != nil,
		InString:     inString,
		InImportPath: inString && cn.ImportSpec != nil,
		PkgName:      pkgname,
	}
	return cx
}
