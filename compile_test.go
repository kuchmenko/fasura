package fasura

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const transferABIJSON = `[{"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Transfer","type":"event"}]`

func TestCompileConfigMapsORClausesToPositionalQueries(t *testing.T) {
	t.Parallel()

	contractABI := mustABI(t, transferABIJSON)
	contract := common.HexToAddress("0x1000000000000000000000000000000000000001")
	wallet := common.HexToAddress("0x2000000000000000000000000000000000000002")
	compiled, err := compileConfig(Config{
		Chains: map[string]Chain{"ethereum": {ID: 1, RPCURL: "ws://example.test"}},
		Sources: []Source{{
			Name: "token",
			ABI:  contractABI,
			Deployments: map[string]Deployment{
				"ethereum": {Addresses: []common.Address{contract}},
			},
			Events: []EventConfig{{
				Name: "Transfer",
				Match: []Match{
					{"from": []any{wallet.Hex()}},
					{"to": []any{wallet.Hex()}},
				},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	stream := compiled.chains["ethereum"].streams[0]
	if len(stream.queries) != 2 {
		t.Fatalf("query count = %d, want 2", len(stream.queries))
	}
	walletTopic := common.BytesToHash(wallet.Bytes())
	if got := stream.queries[0].Topics; len(got) != 2 || got[1][0] != walletTopic {
		t.Fatalf("from query topics = %#v", got)
	}
	if got := stream.queries[1].Topics; len(got) != 3 || got[1] != nil || got[2][0] != walletTopic {
		t.Fatalf("to query topics = %#v", got)
	}
	if stream.queries[0].Addresses[0] != contract || stream.queries[1].Addresses[0] != contract {
		t.Fatal("contract address missing from compiled queries")
	}
}

func TestCompileConfigRejectsNonIndexedMatch(t *testing.T) {
	t.Parallel()

	_, err := compileConfig(Config{
		Chains: map[string]Chain{"ethereum": {ID: 1, RPCURL: "ws://example.test"}},
		Sources: []Source{{
			Name:        "token",
			ABI:         mustABI(t, transferABIJSON),
			Deployments: map[string]Deployment{"ethereum": {Addresses: []common.Address{common.HexToAddress("0x1")}}},
			Events:      []EventConfig{{Name: "Transfer", Match: []Match{{"value": []any{big.NewInt(10)}}}}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), `match field "value" is not indexed`) {
		t.Fatalf("error = %v, want non-indexed match error", err)
	}
}

func TestCompileConfigRejectsUnknownDeploymentChain(t *testing.T) {
	t.Parallel()

	_, err := compileConfig(Config{
		Chains: map[string]Chain{"ethereum": {ID: 1, RPCURL: "ws://example.test"}},
		Sources: []Source{{
			Name:        "token",
			ABI:         mustABI(t, transferABIJSON),
			Deployments: map[string]Deployment{"polygon": {Addresses: []common.Address{common.HexToAddress("0x1")}}},
			Events:      []EventConfig{{Name: "Transfer"}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown chain "polygon"`) {
		t.Fatalf("error = %v, want unknown chain error", err)
	}
}

func TestCompileConfigRejectsEventInputsThatCannotBeDecodedByName(t *testing.T) {
	t.Parallel()

	unnamedABI := mustABI(t, `[{"anonymous":false,"inputs":[{"indexed":true,"name":"value","type":"address"}],"name":"Changed","type":"event"}]`)
	unnamedEvent := unnamedABI.Events["Changed"]
	unnamedEvent.Inputs[0].Name = ""
	unnamedABI.Events["Changed"] = unnamedEvent

	tests := []struct {
		name string
		abi  abi.ABI
		want string
	}{
		{
			name: "unnamed",
			abi:  unnamedABI,
			want: "unnamed input",
		},
		{
			name: "duplicate",
			abi:  mustABI(t, `[{"anonymous":false,"inputs":[{"indexed":true,"name":"value","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Changed","type":"event"}]`),
			want: `duplicate input name "value"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := compileConfig(Config{
				Chains: map[string]Chain{"ethereum": {ID: 1, RPCURL: "ws://example.test"}},
				Sources: []Source{{
					Name:        "contract",
					ABI:         test.abi,
					Deployments: map[string]Deployment{"ethereum": {Addresses: []common.Address{common.HexToAddress("0x1")}}},
					Events:      []EventConfig{{Name: "Changed"}},
				}},
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestConvertMatchValueParsesBooleanStrings(t *testing.T) {
	t.Parallel()

	contractABI := mustABI(t, `[{"anonymous":false,"inputs":[{"indexed":true,"name":"enabled","type":"bool"}],"name":"Changed","type":"event"}]`)
	boolType := contractABI.Events["Changed"].Inputs[0].Type
	for _, raw := range []string{"true", "false"} {
		got, err := convertMatchValue(boolType, raw)
		if err != nil {
			t.Fatalf("convert %q: %v", raw, err)
		}
		if got != (raw == "true") {
			t.Fatalf("convert %q = %v", raw, got)
		}
	}
	if _, err := convertMatchValue(boolType, "not-a-boolean"); err == nil {
		t.Fatal("invalid boolean string accepted")
	}
}

func mustABI(t *testing.T, definition string) abi.ABI {
	t.Helper()
	contractABI, err := abi.JSON(strings.NewReader(definition))
	if err != nil {
		t.Fatal(err)
	}
	return contractABI
}
