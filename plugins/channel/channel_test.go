package channel

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/kuchmenko/fasura"
)

func TestPluginForwardsEvent(t *testing.T) {
	output := make(chan fasura.Event, 1)
	plugin := New(output)

	event := fasura.Event{Source: "token", Name: "Transfer", Address: common.HexToAddress("0x1")}
	if err := plugin.handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if got := <-output; got.Key() != event.Key() {
		t.Fatalf("forwarded event = %s, want %s", got.Key(), event.Key())
	}
}
