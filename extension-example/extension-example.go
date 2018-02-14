package margo

import (
	"disposa.blue/margo/golang"
	"disposa.blue/margo/mg"
	"time"
)

func Margo(mx mg.Ctx) {
	mx.Store.
		// add our reducers (plugins)
		// reducers are run in the specified order
		Use(
			DayTimeStatus,

			// both GoFmt and GoImports will automatically disable the GoSublime version
			// use the new gofmt integration instead of the old GoSublime version
			golang.GoFmt,
			// or
			// golang.GoImports,
		)
}

// DayTimeStatus adds the current day and time to the status bar
var DayTimeStatus = mg.Reduce(func(mx *mg.Ctx) *mg.State {
	if _, ok := mx.Action.(mg.Started); ok {
		dispatch := mx.Store.Dispatch
		// kick off the ticker when we start
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			for range ticker.C {
				dispatch(mg.Render)
			}
		}()
	}

	// we always want to render the time
	// otherwise it will sometimes disappear from the status bar
	now := time.Now()
	format := "Mon, 15:04"
	if now.Second()%2 == 0 {
		format = "Mon, 15 04"
	}
	return mx.AddStatus(now.Format(format))
})
