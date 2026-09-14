package fasura

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestRunStreamDecodesAndDeduplicatesOverlappingMatches(t *testing.T) {
	t.Parallel()

	contractABI := mustABI(t, transferABIJSON)
	definition := contractABI.Events["Transfer"]
	contract := common.HexToAddress("0x1000000000000000000000000000000000000001")
	wallet := common.HexToAddress("0x2000000000000000000000000000000000000002")
	stream := mustCompiledStream(t, contractABI, contract, wallet)
	client := newFakeClient(2)
	stop := errors.New("subscription stopped for test")

	var mu sync.Mutex
	var events []Event
	done := make(chan error, 1)
	go func() {
		done <- runStream(context.Background(), client, stream, map[string][]Handler{
			"token.Transfer": {
				func(_ context.Context, event Event) error {
					mu.Lock()
					events = append(events, event)
					mu.Unlock()
					return nil
				},
			},
		}, nil)
	}()

	client.waitForSubscriptions(t)
	log := transferLog(t, definition, contract, wallet, wallet, big.NewInt(17))
	client.output <- log
	client.output <- log
	client.subscriptions[0].errors <- stop

	select {
	case err := <-done:
		if !errors.Is(err, stop) {
			t.Fatalf("runStream error = %v, want wrapped stop", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runStream did not stop")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	event := events[0]
	if event.Key() != "token.Transfer" || event.Chain != "ethereum" || event.ChainID != 1 || event.Address != contract {
		t.Fatalf("event identity = %+v", event)
	}
	if got := event.Values["from"].(common.Address); got != wallet {
		t.Fatalf("from = %s, want %s", got, wallet)
	}
	if got := event.Values["value"].(*big.Int); got.Cmp(big.NewInt(17)) != 0 {
		t.Fatalf("value = %s, want 17", got)
	}
}

func TestAppRunAlreadyCanceledDoesNotDial(t *testing.T) {
	t.Parallel()

	app, err := New(Config{
		Chains: map[string]Chain{"ethereum": {ID: 1, RPCURL: "ws://must-not-be-dialed.example"}},
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
	if err := app.HandleAll(func(context.Context, Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	app.dial = func(context.Context, string) (rpcClient, error) {
		t.Fatal("dial called after cancellation")
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run error = %v, want nil", err)
	}
}

func TestAppRunRejectsRPCForWrongChain(t *testing.T) {
	t.Parallel()

	app, err := New(Config{
		Chains: map[string]Chain{"ethereum": {ID: 1, RPCURL: "ws://polygon.example"}},
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
	if err := app.HandleAll(func(context.Context, Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	app.dial = func(context.Context, string) (rpcClient, error) {
		return chainIDClient{id: big.NewInt(137)}, nil
	}
	err = app.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), `chain "ethereum" id is 137, want 1`) {
		t.Fatalf("Run error = %v, want chain ID mismatch", err)
	}
}

func TestRunStreamTreatsSubscriptionSetupCancellationAsCleanShutdown(t *testing.T) {
	t.Parallel()

	contractABI := mustABI(t, transferABIJSON)
	stream := mustCompiledStream(
		t,
		contractABI,
		common.HexToAddress("0x1000000000000000000000000000000000000001"),
		common.HexToAddress("0x2000000000000000000000000000000000000002"),
	)
	ctx, cancel := context.WithCancel(context.Background())
	client := cancelOnSubscribeClient{cancel: cancel}
	if err := runStream(ctx, client, stream, nil, nil); err != nil {
		t.Fatalf("runStream error = %v, want nil", err)
	}
}

func TestAppRequiresHandlerForEveryConfiguredEvent(t *testing.T) {
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
	err = app.Run(context.Background())
	if err == nil || err.Error() != `event "token.Transfer" has no handler` {
		t.Fatalf("Run error = %v, want missing handler error", err)
	}
}

func mustCompiledStream(t *testing.T, contractABI abi.ABI, contract, wallet common.Address) compiledStream {
	t.Helper()
	compiled, err := compileConfig(Config{
		Chains: map[string]Chain{"ethereum": {ID: 1, RPCURL: "ws://example.test"}},
		Sources: []Source{{
			Name:        "token",
			ABI:         contractABI,
			Deployments: map[string]Deployment{"ethereum": {Addresses: []common.Address{contract}}},
			Events: []EventConfig{{Name: "Transfer", Match: []Match{
				{"from": []any{wallet.Hex()}},
				{"to": []any{wallet.Hex()}},
			}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return compiled.chains["ethereum"].streams[0]
}

func transferLog(t *testing.T, definition abi.Event, contract, from, to common.Address, value *big.Int) types.Log {
	t.Helper()
	data, err := definition.Inputs.NonIndexed().Pack(value)
	if err != nil {
		t.Fatal(err)
	}
	return types.Log{
		Address:     contract,
		Topics:      []common.Hash{definition.ID, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())},
		Data:        data,
		BlockNumber: 10,
		BlockHash:   common.HexToHash("0x10"),
		TxHash:      common.HexToHash("0x20"),
		Index:       3,
	}
}

type fakeClient struct {
	output        chan<- types.Log
	subscribed    chan struct{}
	subscriptions []*fakeSubscription
	mu            sync.Mutex
	want          int
}

func newFakeClient(want int) *fakeClient {
	return &fakeClient{subscribed: make(chan struct{}), want: want}
}

func (client *fakeClient) SubscribeFilterLogs(_ context.Context, _ ethereum.FilterQuery, output chan<- types.Log) (ethereum.Subscription, error) {
	client.mu.Lock()
	subscription := &fakeSubscription{errors: make(chan error, 1)}
	client.subscriptions = append(client.subscriptions, subscription)
	client.output = output
	if len(client.subscriptions) == client.want {
		close(client.subscribed)
	}
	client.mu.Unlock()
	return subscription, nil
}

func (client *fakeClient) ChainID(context.Context) (*big.Int, error) {
	return big.NewInt(1), nil
}

func (client *fakeClient) Close() {}

type chainIDClient struct {
	id *big.Int
}

func (client chainIDClient) ChainID(context.Context) (*big.Int, error) {
	return client.id, nil
}

func (chainIDClient) SubscribeFilterLogs(context.Context, ethereum.FilterQuery, chan<- types.Log) (ethereum.Subscription, error) {
	return nil, errors.New("unexpected subscription")
}

func (chainIDClient) Close() {}

type cancelOnSubscribeClient struct {
	cancel context.CancelFunc
}

func (client cancelOnSubscribeClient) ChainID(context.Context) (*big.Int, error) {
	return big.NewInt(1), nil
}

func (client cancelOnSubscribeClient) SubscribeFilterLogs(ctx context.Context, _ ethereum.FilterQuery, _ chan<- types.Log) (ethereum.Subscription, error) {
	client.cancel()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (cancelOnSubscribeClient) Close() {}

func (client *fakeClient) waitForSubscriptions(t *testing.T) {
	t.Helper()
	select {
	case <-client.subscribed:
	case <-time.After(time.Second):
		t.Fatal("subscriptions were not created")
	}
}

type fakeSubscription struct {
	once   sync.Once
	errors chan error
}

func (subscription *fakeSubscription) Err() <-chan error {
	return subscription.errors
}

func (subscription *fakeSubscription) Unsubscribe() {
	subscription.once.Do(func() { close(subscription.errors) })
}
