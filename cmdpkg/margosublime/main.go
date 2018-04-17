package margosublime

import (
	"fmt"

	"github.com/urfave/cli"
	"margo.sh/mg"
	"margo.sh/mgcli"
	"margo.sh/sublime"
)

var (
	margoExt mg.MargoFunc    = sublime.Margo
	sublCfg  mg.EditorConfig = sublime.DefaultConfig
)

func Main() {
	cfg := mg.AgentConfig{AgentName: sublime.AgentName}
	app := mgcli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "codec",
			Value:       cfg.Codec,
			Destination: &cfg.Codec,
			Usage:       fmt.Sprintf("The IPC codec: %s (default %s)", mg.CodecNamesStr, mg.DefaultCodec),
		},
	}
	app.Action = func(ctx *cli.Context) error {
		if ctx.Args().Present() {
			return cli.ShowAppHelp(ctx)
		}

		ag, err := mg.NewAgent(cfg)
		if err != nil {
			return mgcli.Error("agent creation failed:", err)
		}

		ag.Store.EditorConfig(sublCfg)
		if margoExt != nil {
			margoExt(ag.Args())
		}

		if err := ag.Run(); err != nil {
			return mgcli.Error("agent failed:", err)
		}
		return nil
	}
	app.RunAndExitOnError()
}
