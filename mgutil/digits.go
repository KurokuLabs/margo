package mgutil

var (
	PrimaryDigits   = DigitDisplay{'🄌', '➊', '➋', '➌', '➍', '➎', '➏', '➐', '➑', '➒'}
	SecondaryDigits = DigitDisplay{'🄋', '➀', '➁', '➂', '➃', '➄', '➅', '➆', '➇', '➈'}
)

type DigitDisplay []rune

func (p DigitDisplay) Draw(n int, f func(rune)) {
	base := len(p)
	if n < base {
		f(p[n])
		return
	}
	m := n / base
	p.Draw(m, f)
	f(p[n-m*base])
}
