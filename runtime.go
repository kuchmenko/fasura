package fasura

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/sync/errgroup"
)

type rpcClient interface {
	ChainID(context.Context) (*big.Int, error)
	SubscribeFilterLogs(context.Context, ethereum.FilterQuery, chan<- types.Log) (ethereum.Subscription, error)
	Close()
}

type dialClient func(context.Context, string) (rpcClient, error)

func defaultDialClient(ctx context.Context, rpcURL string) (rpcClient, error) {
	return ethclient.DialContext(ctx, rpcURL)
}

// Run starts all configured chain subscriptions and plugin workers. It blocks
// until the context is canceled or any connection, subscription, worker, or
// handler fails. Run does not reconnect because doing so without backfilling
// the subscription gap could silently lose logs.
func (app *App) Run(ctx context.Context) error {
	handlers, all, workers, err := app.prepareRun()
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}

	group, groupCtx := errgroup.WithContext(ctx)
	for _, chain := range app.config.chains {
		chain := chain
		group.Go(func() error {
			client, err := app.dial(groupCtx, chain.rpcURL)
			if err != nil {
				if groupCtx.Err() != nil {
					return nil
				}
				return fmt.Errorf("connect to chain %q: %w", chain.name, err)
			}
			defer client.Close()
			chainID, err := client.ChainID(groupCtx)
			if err != nil {
				if groupCtx.Err() != nil && errors.Is(err, groupCtx.Err()) {
					return nil
				}
				return fmt.Errorf("read chain ID for chain %q: %w", chain.name, err)
			}
			if chainID.Cmp(new(big.Int).SetUint64(chain.id)) != 0 {
				return fmt.Errorf("chain %q id is %s, want %d", chain.name, chainID, chain.id)
			}
			return runChain(groupCtx, client, chain, handlers, all)
		})
	}
	for _, worker := range workers {
		worker := worker
		group.Go(func() error {
			err := worker(groupCtx)
			if groupCtx.Err() != nil && (err == nil || errors.Is(err, groupCtx.Err())) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("plugin worker: %w", err)
			}
			return errors.New("plugin worker stopped")
		})
	}
	return group.Wait()
}

func runChain(ctx context.Context, client rpcClient, chain compiledChain, handlers map[string][]Handler, all []Handler) error {
	group, groupCtx := errgroup.WithContext(ctx)
	for _, stream := range chain.streams {
		stream := stream
		group.Go(func() error {
			return runStream(groupCtx, client, stream, handlers, all)
		})
	}
	return group.Wait()
}

func runStream(ctx context.Context, client rpcClient, stream compiledStream, handlers map[string][]Handler, all []Handler) error {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	logs := make(chan types.Log)
	subscriptionErrors := make(chan error, len(stream.queries))
	subscriptions := make([]ethereum.Subscription, 0, len(stream.queries))
	for index, query := range stream.queries {
		subscription, err := client.SubscribeFilterLogs(streamCtx, query, logs)
		if err != nil {
			for _, active := range subscriptions {
				active.Unsubscribe()
			}
			if streamCtx.Err() != nil && errors.Is(err, streamCtx.Err()) {
				return nil
			}
			return fmt.Errorf("subscribe to %s on chain %q query %d: %w", stream.source+"."+stream.event.Name, stream.chainName, index, err)
		}
		subscriptions = append(subscriptions, subscription)
		go watchSubscription(streamCtx, subscription, subscriptionErrors)
	}
	defer func() {
		for _, subscription := range subscriptions {
			subscription.Unsubscribe()
		}
	}()

	duplicates := make(map[logKey]int)
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-subscriptionErrors:
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("subscription %s on chain %q stopped: %w", stream.source+"."+stream.event.Name, stream.chainName, err)
		case log := <-logs:
			key := makeLogKey(log)
			if remaining, duplicate := duplicates[key]; duplicate {
				if remaining == 1 {
					delete(duplicates, key)
				} else {
					duplicates[key] = remaining - 1
				}
				continue
			}

			matches := matchingClauses(log.Topics, stream.clauses)
			if matches == 0 {
				continue
			}
			if matches > 1 {
				// One RPC subscription exists per OR clause. Remember exactly how
				// many duplicate deliveries remain for a log matching several clauses.
				duplicates[key] = matches - 1
			}

			event, err := decodeEvent(stream, log)
			if err != nil {
				return err
			}
			if err := dispatch(streamCtx, event, handlers, all); err != nil {
				if streamCtx.Err() != nil && errors.Is(err, streamCtx.Err()) {
					return nil
				}
				return fmt.Errorf("handle %s on chain %q: %w", event.Key(), stream.chainName, err)
			}
		}
	}
}

func watchSubscription(ctx context.Context, subscription ethereum.Subscription, output chan<- error) {
	select {
	case <-ctx.Done():
	case err, ok := <-subscription.Err():
		if !ok {
			err = errors.New("subscription closed")
		} else if err == nil {
			err = errors.New("subscription stopped without an error")
		}
		select {
		case <-ctx.Done():
		case output <- err:
		}
	}
}

func decodeEvent(stream compiledStream, log types.Log) (Event, error) {
	if len(log.Topics) == 0 || log.Topics[0] != stream.event.ID {
		return Event{}, fmt.Errorf("decode %s on chain %q: unexpected event topic", stream.source+"."+stream.event.Name, stream.chainName)
	}
	indexed := indexedArguments(stream.event.Inputs)
	if len(log.Topics) != len(indexed)+1 {
		return Event{}, fmt.Errorf("decode %s on chain %q: got %d topics, want %d", stream.source+"."+stream.event.Name, stream.chainName, len(log.Topics), len(indexed)+1)
	}
	values := make(map[string]any, len(stream.event.Inputs))
	if err := abi.ParseTopicsIntoMap(values, indexed, log.Topics[1:]); err != nil {
		return Event{}, fmt.Errorf("decode indexed values for %s on chain %q: %w", stream.source+"."+stream.event.Name, stream.chainName, err)
	}
	if err := stream.event.Inputs.NonIndexed().UnpackIntoMap(values, log.Data); err != nil {
		return Event{}, fmt.Errorf("decode data values for %s on chain %q: %w", stream.source+"."+stream.event.Name, stream.chainName, err)
	}
	return Event{
		Source:  stream.source,
		Name:    stream.event.Name,
		Chain:   stream.chainName,
		ChainID: stream.chainID,
		Address: log.Address,
		Values:  values,
		Log:     log,
	}, nil
}

func indexedArguments(arguments abi.Arguments) abi.Arguments {
	result := make(abi.Arguments, 0, len(arguments))
	for _, argument := range arguments {
		if argument.Indexed {
			result = append(result, argument)
		}
	}
	return result
}

func matchingClauses(topics []common.Hash, clauses []topicClause) int {
	matches := 0
	for _, clause := range clauses {
		matched := true
		for position, allowed := range clause {
			if position >= len(topics) {
				matched = false
				break
			}
			if _, ok := allowed[topics[position]]; !ok {
				matched = false
				break
			}
		}
		if matched {
			matches++
		}
	}
	return matches
}

type logKey struct {
	blockHash common.Hash
	txHash    common.Hash
	address   common.Address
	index     uint
	removed   bool
}

func makeLogKey(log types.Log) logKey {
	return logKey{
		blockHash: log.BlockHash,
		txHash:    log.TxHash,
		address:   log.Address,
		index:     log.Index,
		removed:   log.Removed,
	}
}
