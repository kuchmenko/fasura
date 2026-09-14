package fasura_test

import (
	"testing"

	"github.com/kuchmenko/fasura"
	"github.com/kuchmenko/fasura/configyaml"
)

func TestExampleConfigurationsLoadAndCompile(t *testing.T) {
	t.Setenv("ETHEREUM_WS_RPC_URL", "ws://ethereum.example")
	t.Setenv("POLYGON_WS_RPC_URL", "ws://polygon.example")
	t.Setenv("WATCHED_ADDRESS", "0x000000000000000000000000000000000000dEaD")
	t.Setenv("UNISWAP_V4_POOL_ID", "0x0000000000000000000000000000000000000000000000000000000000000001")

	paths := []string{
		"examples/ctf/fasura.yaml",
		"examples/erc20/fasura.yaml",
		"examples/uniswap-v3/fasura.yaml",
		"examples/uniswap-v4/fasura.yaml",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			config, err := configyaml.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fasura.New(config); err != nil {
				t.Fatal(err)
			}
		})
	}
}
