# Fasura

Fasura is a small Go library for low-latency, filtered EVM log streams. It
shares one RPC connection between named streams on a chain and passes each raw
log to application code.

Fasura is not an indexer. It does not persist checkpoints, backfill gaps, or
hide reconnects that could lose events. Use an indexer such as Envio when an
application also needs complete historical and canonical state.

## Install

```sh
go get github.com/kuchmenko/fasura
```

## Use

```go
config := fasura.Config{Chains: []fasura.Chain{{
    ID:     8453,
    RPCURL: os.Getenv("BASE_WS_RPC_URL"),
    Streams: []fasura.Stream{{
        Name: "swaps",
        Query: ethereum.FilterQuery{
            Addresses: []common.Address{pool},
            Topics:    [][]common.Hash{{swapTopic}},
        },
    }},
}}}

err := fasura.Run(ctx, config, func(ctx context.Context, event fasura.Event) error {
    return handleSwap(ctx, event.Log)
})
```

`FilterQuery` runs at the RPC provider. Fasura tags each matching log with its
chain ID and stream name. `Route` can dispatch several named streams:

```go
routes := fasura.Route{
    "ctf-fills": handleOrderFilled,
    "swaps":     handleSwap,
}

err := fasura.Run(ctx, config, routes.Handle)
```

Calls stay sequential within one stream. Different streams can call the shared
handler concurrently. A connection, subscription, or handler error stops the
whole `Run` call. Context cancellation stops it without an error.

Use `fasura.Send(events)` when a caller-owned Go channel is the desired output.

## Examples

- [`examples/ctf-listener`](examples/ctf-listener) loads TOML, filters indexed
  maker addresses, decodes CTF Exchange V1/V2 events, and routes them by chain.
- [`examples/exchanges`](examples/exchanges) creates separate named streams for
  two exchange versions on one chain.
- [`examples/multichain`](examples/multichain) handles one logical stream from
  Base and Polygon.
- [`examples/filters`](examples/filters) combines contract, event, and indexed
  topic filters.

Run the CTF listener with the repository config:

```sh
cp .env.example .env
go run ./examples/ctf-listener -config fasura.toml
```

The TOML loader and ABI-specific models intentionally belong to the example,
not the core package. SSE, WebSocket, and gRPC servers can consume the handler
API from separate transport packages without changing log subscriptions.

## Planned production work

- reconnect gap backfill and checkpoints;
- dynamic stream updates;
- reorg buffering and finalized block watermarks;
- transport adapters with explicit slow-client policies.
