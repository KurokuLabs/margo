package margo

import (
	"margo.sh/mgcli"
	"margo.sh/sublime"
	"github.com/urfave/cli"
)

func Main() {
	app := mgcli.NewApp()
	app.Commands = []cli.Command{
		sublime.Command,
	}
	app.RunAndExitOnError()
}
