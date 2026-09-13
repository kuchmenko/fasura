// Package channel forwards every Fasura event to a Go channel.
package channel

import (
	"context"
	"errors"

	"github.com/kuchmenko/fasura"
)

// Plugin forwards events to a caller-owned channel.
type Plugin struct {
	output chan<- fasura.Event
}

// New creates a blocking channel output plugin.
func New(output chan<- fasura.Event) *Plugin {
	return &Plugin{output: output}
}

// Register implements fasura.Plugin.
func (plugin *Plugin) Register(app *fasura.App) error {
	if plugin.output == nil {
		return errors.New("output channel is required")
	}
	return app.HandleAll(plugin.handle)
}

func (plugin *Plugin) handle(ctx context.Context, event fasura.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case plugin.output <- event:
		return nil
	}
}
