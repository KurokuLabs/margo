package golang

import (
	"disposa.blue/margo/mg"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/ioutil"
)

const (
	ParseFileMode = parser.ParseComments | parser.DeclarationErrors | parser.AllErrors
)

var (
	NilAstFile   = &ast.File{}
	NilTokenFile = &token.File{}
)

type ParsedFile struct {
	AstFile   *ast.File
	TokenFile *token.File
	Error     error
	ErrorList scanner.ErrorList
}

func ParseFile(sto *mg.Store, fn string, src []byte) *ParsedFile {
	mode := ParseFileMode
	if len(src) == 0 {
		var err error
		src, err = ioutil.ReadFile(fn)
		if err != nil {
			return &ParsedFile{
				AstFile:   NilAstFile,
				TokenFile: NilTokenFile,
				Error:     err,
			}
		}
	}

	type key struct{ hash string }
	k := key{mg.SrcHash(src)}
	if sto != nil {
		if pf, ok := sto.Get(k).(*ParsedFile); ok {
			return pf
		}
	}

	fset := token.NewFileSet()
	pf := &ParsedFile{}
	pf.AstFile, pf.Error = parser.ParseFile(fset, fn, src, mode)
	pf.TokenFile = fset.File(pf.AstFile.Pos())
	pf.ErrorList, _ = pf.Error.(scanner.ErrorList)
	if pf.AstFile == nil {
		pf.AstFile = NilAstFile
	}
	if pf.TokenFile == nil {
		pf.TokenFile = NilTokenFile
	}

	if sto != nil {
		sto.Put(k, pf)
	}

	return pf
}
