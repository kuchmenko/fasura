package fasura

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestPluginRegistersHandlerAndWorker(t *testing.T) {
	t.Parallel()

	app, err := New(Config{
		Chains: map[string]Chain{"ethereum": {ID: 1, RPCURL: "ws://example.test"}},
		Sources: []Source{{
			Name:        "token",
			ABI:         mustABI(t, transferABIJSON),
			Deployments: map[string]Deployment{"ethereum": {Addresses: []common.Address{common.HexToAddress("0x1")}}},
			Events:      []EventConfig{{Name: "Transfer"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plugin := testPlugin{register: func(app *App) error {
		if err := app.HandleAll(func(context.Context, Event) error { return nil }); err != nil {
			return err
		}
		return app.AddWorker(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
	}}
	if err := app.Use(plugin); err != nil {
		t.Fatal(err)
	}
	if len(app.all) != 1 || len(app.workers) != 1 {
		t.Fatalf("registered handlers/workers = (%d, %d), want (1, 1)", len(app.all), len(app.workers))
	}
}

type testPlugin struct {
	register func(*App) error
}

func (plugin testPlugin) Register(app *App) error {
	return plugin.register(app)
}
