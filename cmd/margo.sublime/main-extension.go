// +build margo_extension

package main

import (
	"margo"
)

func init() {
	initFuncs = append(initFuncs, margo.Init)
}
