package fasura

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestRunStreamTagsEventsAndPreservesOrder(t *testing.T) {
	t.Parallel()

	query := ethereum.FilterQuery{
		Addresses: []common.Address{common.HexToAddress("0x1234")},
		Topics:    [][]common.Hash{{common.HexToHash("0xabcd")}},
	}
	client := &fakeSubscriber{
		logs: []types.Log{{Index: 4}, {Index: 9}},
	}
	stop := errors.New("stop after expected events")
	var events []Event

	err := runStream(context.Background(), client, 8453, Stream{Name: "swaps", Query: query}, func(_ context.Context, event Event) error {
		events = append(events, event)
		if len(events) == 2 {
			return stop
		}
		return nil
	})

	if !errors.Is(err, stop) {
		t.Fatalf("runStream error = %v, want wrapped stop error", err)
	}
	if !reflect.DeepEqual(client.query, query) {
		t.Fatalf("subscription query = %#v, want %#v", client.query, query)
	}
	if got := []uint{events[0].Log.Index, events[1].Log.Index}; !reflect.DeepEqual(got, []uint{4, 9}) {
		t.Fatalf("event order = %v, want [4 9]", got)
	}
	for _, event := range events {
		if event.ChainID != 8453 || event.Stream != "swaps" {
			t.Fatalf("event identity = (%d, %q), want (8453, %q)", event.ChainID, event.Stream, "swaps")
		}
	}
}

func TestRunStreamReturnsNilWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runStream(ctx, &fakeSubscriber{}, 1, Stream{Name: "logs"}, func(context.Context, Event) error {
		t.Fatal("handler called after cancellation")
		return nil
	})
	if err != nil {
		t.Fatalf("runStream error = %v, want nil", err)
	}
}

func TestRunReturnsNilWhenAlreadyCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Run(ctx, Config{Chains: []Chain{{
		ID:      1,
		RPCURL:  "ws://must-not-be-dialed.example",
		Streams: []Stream{{Name: "logs"}},
	}}}, func(context.Context, Event) error { return nil })
	if err != nil {
		t.Fatalf("Run error = %v, want nil", err)
	}
}

func TestValidateRejectsDuplicateStreamNamesWithinChain(t *testing.T) {
	t.Parallel()

	err := validate(Config{Chains: []Chain{{
		ID:     1,
		RPCURL: "ws://example.test",
		Streams: []Stream{
			{Name: "fills"},
			{Name: "fills"},
		},
	}}}, func(context.Context, Event) error { return nil })

	if err == nil || !strings.Contains(err.Error(), `duplicate stream name "fills"`) {
		t.Fatalf("validate error = %v, want duplicate stream error", err)
	}
}

func TestRouteDispatchesMatchingStream(t *testing.T) {
	t.Parallel()

	var called string
	route := Route{
		"fills": func(_ context.Context, event Event) error {
			called = event.Stream
			return nil
		},
	}

	if err := route.Handle(context.Background(), Event{Stream: "unknown"}); err != nil {
		t.Fatalf("unknown stream error = %v, want nil", err)
	}
	if called != "" {
		t.Fatalf("unknown stream called %q route", called)
	}
	if err := route.Handle(context.Background(), Event{Stream: "fills"}); err != nil {
		t.Fatalf("matching stream error = %v, want nil", err)
	}
	if called != "fills" {
		t.Fatalf("called route = %q, want fills", called)
	}
}

type fakeSubscriber struct {
	mu    sync.Mutex
	query ethereum.FilterQuery
	logs  []types.Log
}

func (client *fakeSubscriber) SubscribeFilterLogs(ctx context.Context, query ethereum.FilterQuery, output chan<- types.Log) (ethereum.Subscription, error) {
	client.mu.Lock()
	client.query = query
	logs := append([]types.Log(nil), client.logs...)
	client.mu.Unlock()

	subscription := &fakeSubscription{errors: make(chan error)}
	go func() {
		for _, log := range logs {
			select {
			case <-ctx.Done():
				return
			case output <- log:
			}
		}
	}()

	return subscription, nil
}

type fakeSubscription struct {
	once   sync.Once
	errors chan error
}

func (subscription *fakeSubscription) Err() <-chan error {
	return subscription.errors
}

func (subscription *fakeSubscription) Unsubscribe() {
	subscription.once.Do(func() {
		close(subscription.errors)
	})
}
