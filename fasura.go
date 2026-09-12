// Package fasura streams filtered EVM logs to application code.
package fasura

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/sync/errgroup"
)

// Config describes all chains and streams handled by one Run call.
type Config struct {
	Chains []Chain
}

// Chain groups streams that share one RPC connection.
type Chain struct {
	ID      uint64
	RPCURL  string
	Streams []Stream
}

// Stream is one named RPC log subscription.
type Stream struct {
	Name  string
	Query ethereum.FilterQuery
}

// Event identifies the stream and chain that produced a raw EVM log.
type Event struct {
	ChainID uint64
	Stream  string
	Log     types.Log
}

// Handler processes one event. Run may call a handler concurrently for
// different streams, but calls stay sequential within each stream.
type Handler func(context.Context, Event) error

// Route dispatches events by stream name. Events without a route are ignored.
type Route map[string]Handler

// Handle implements Handler.
func (route Route) Handle(ctx context.Context, event Event) error {
	handler, ok := route[event.Stream]
	if !ok {
		return nil
	}
	if handler == nil {
		return fmt.Errorf("stream %q has a nil handler", event.Stream)
	}

	return handler(ctx, event)
}

// Send adapts a Go channel to a Handler.
func Send(output chan<- Event) Handler {
	return func(ctx context.Context, event Event) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case output <- event:
			return nil
		}
	}
}

// Run subscribes to every configured stream and blocks until the context is
// canceled or a connection, subscription, or handler fails. Run does not retry
// because retrying without backfilling the subscription gap can lose logs.
func Run(ctx context.Context, config Config, handler Handler) error {
	if err := validate(config, handler); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}

	group, groupCtx := errgroup.WithContext(ctx)
	for _, chain := range config.Chains {
		chain := chain
		group.Go(func() error {
			client, err := ethclient.DialContext(groupCtx, chain.RPCURL)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("connect to chain %d: %w", chain.ID, err)
			}
			defer client.Close()

			return runChain(groupCtx, client, chain, handler)
		})
	}

	return group.Wait()
}

type logSubscriber interface {
	SubscribeFilterLogs(context.Context, ethereum.FilterQuery, chan<- types.Log) (ethereum.Subscription, error)
}

func runChain(ctx context.Context, client logSubscriber, chain Chain, handler Handler) error {
	group, groupCtx := errgroup.WithContext(ctx)
	for _, stream := range chain.Streams {
		stream := stream
		group.Go(func() error {
			return runStream(groupCtx, client, chain.ID, stream, handler)
		})
	}

	return group.Wait()
}

func runStream(ctx context.Context, client logSubscriber, chainID uint64, stream Stream, handler Handler) error {
	logs := make(chan types.Log)
	subscription, err := client.SubscribeFilterLogs(ctx, stream.Query, logs)
	if err != nil {
		return fmt.Errorf("subscribe to stream %q on chain %d: %w", stream.Name, chainID, err)
	}
	defer subscription.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-subscription.Err():
			if ctx.Err() != nil {
				return nil
			}
			if !ok {
				return fmt.Errorf("stream %q on chain %d: subscription closed", stream.Name, chainID)
			}
			return fmt.Errorf("stream %q on chain %d: subscription stopped: %w", stream.Name, chainID, err)
		case log, ok := <-logs:
			if !ok {
				return fmt.Errorf("stream %q on chain %d: log channel closed", stream.Name, chainID)
			}

			err := handler(ctx, Event{ChainID: chainID, Stream: stream.Name, Log: log})
			if err != nil {
				if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
					return nil
				}
				return fmt.Errorf("handle stream %q on chain %d: %w", stream.Name, chainID, err)
			}
		}
	}
}

func validate(config Config, handler Handler) error {
	if handler == nil {
		return errors.New("handler is required")
	}

	for _, chain := range config.Chains {
		if chain.RPCURL == "" {
			return fmt.Errorf("chain %d: RPC URL is required", chain.ID)
		}

		names := make(map[string]struct{}, len(chain.Streams))
		for streamIndex, stream := range chain.Streams {
			if stream.Name == "" {
				return fmt.Errorf("chain %d stream %d: name is required", chain.ID, streamIndex)
			}
			if _, exists := names[stream.Name]; exists {
				return fmt.Errorf("chain %d: duplicate stream name %q", chain.ID, stream.Name)
			}
			names[stream.Name] = struct{}{}
		}
	}

	return nil
}
