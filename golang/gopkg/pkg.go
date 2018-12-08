package gopkg

import (
	"path/filepath"
	"strings"
)

type Pkg struct {
	IsCommand     bool
	ImportablePfx string

	// The following fields are a subset of build.Package
	Dir        string
	Name       string
	ImportPath string
	Standard   bool
}

var (
	internalSepDir = filepath.FromSlash("/internal/")
	vendorSepDir   = filepath.FromSlash("/vendor/")
)

func (p *Pkg) Importable(srcDir string) bool {
	if p.IsCommand {
		return false
	}
	if p.Dir == srcDir {
		return false
	}
	if s := p.ImportablePfx; s != "" {
		return strings.HasPrefix(srcDir, s) || srcDir == s[:len(s)-1]
	}
	return true
}

func (p *Pkg) dirPfx(dir, slash string) string {
	if i := strings.LastIndex(dir, slash); i >= 0 {
		return filepath.Dir(dir[:i+len(slash)-1]) + string(filepath.Separator)
	}
	if d := strings.TrimSuffix(dir, slash[:len(slash)-1]); d != dir {
		return filepath.Dir(d) + string(filepath.Separator)
	}
	return ""
}

func (p *Pkg) Finalize() {
	p.Dir = filepath.Clean(p.Dir)
	p.IsCommand = p.Name == "main"

	// does importing from the 'vendor' and 'internal' dirs work the same?
	// who cares... I'm the supreme, I make the rules in this outpost...
	p.ImportablePfx = p.dirPfx(p.Dir, internalSepDir)
	if p.ImportablePfx == "" {
		p.ImportablePfx = p.dirPfx(p.Dir, vendorSepDir)
	}

	s := p.ImportPath
	switch i := strings.LastIndex(s, "/vendor/"); {
	case i >= 0:
		p.ImportPath = s[i+len("/vendor/"):]
	case strings.HasPrefix(s, "vendor/"):
		p.ImportPath = s[len("vendor/"):]
	}
}
