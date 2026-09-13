# Fasura

Fasura is an embedded Go runtime for low-latency EVM events. Source-centric
YAML selects contract deployments and ABI events. Fasura builds provider-side
filters, decodes matching logs, and sends them to application handlers or
plugins.

Fasura is not an indexer. It does not persist checkpoints, backfill subscription
gaps, hide reconnects, or determine canonical/finalized state. Pair it with an
indexer when an application also needs complete history.

## Install

```sh
go get github.com/kuchmenko/fasura
```

## Configure sources

```yaml
version: 1

chains:
  ethereum:
    id: 1
    rpc: "${ETHEREUM_WS_RPC_URL}"

sources:
  - name: usdc
    abi: ./abis/IERC20.json
    deployments:
      ethereum:
        addresses:
          - "0xA0b86991c6218b36c1d19d4a2e9Eb0cE3606eB48"
    events:
      - name: Transfer
        match:
          - from: ["${WATCHED_ADDRESS}"]
          - to: ["${WATCHED_ADDRESS}"]
```

ABI paths resolve relative to the YAML file. `${VAR}` expands an environment
variable when it is the complete YAML value. Missing variables, unknown YAML
fields, invalid addresses, unknown ABI events, and invalid matches fail before
Fasura connects to a chain.

Each `match` item is an OR clause. Fields inside one item are AND conditions;
values for one field are OR alternatives. Only indexed ABI arguments can be
matched by an RPC provider:

```yaml
match:
  - from: ["0xA...", "0xB..."]
    to: ["0xC..."]
  - from: ["0xD..."]
```

This means `(from IN [A, B] AND to = C) OR from = D`. Fasura creates the
required positional topic queries and suppresses duplicate delivery when one
log matches several clauses. Check non-indexed values in a handler after ABI
decoding.

## Handle events

```go
config, err := configyaml.Load("fasura.yaml")
if err != nil {
    return err
}

app, err := fasura.New(config)
if err != nil {
    return err
}

err = app.Handle("usdc.Transfer", func(ctx context.Context, event fasura.Event) error {
    from := event.Values["from"].(common.Address)
    to := event.Values["to"].(common.Address)
    value := event.Values["value"].(*big.Int)
    return handleTransfer(ctx, from, to, value)
})
if err != nil {
    return err
}

return app.Run(ctx)
```

Handler keys are always `source.event`. Use `HandleAll` for raw forwarding or
shared logging. Handlers for different source, event, and chain streams may run
concurrently. Calls stay sequential within one source event on one chain.

A connection, subscription, plugin worker, or handler error stops the app.
Context cancellation stops it without an error.

## Plugins

Plugins are normal Go packages compiled into the application:

```go
events := make(chan fasura.Event)
if err := app.Use(channel.New(events)); err != nil {
    return err
}
```

A plugin implements:

```go
type Plugin interface {
    Register(*fasura.App) error
}
```

Plugins may register handlers and background workers. This supports custom
processing and separate channel, WebSocket, SSE, or gRPC transport packages
without adding server stacks to the core runtime. The included channel plugin
uses blocking delivery; the caller controls its channel buffer and consumer.

## Examples

- [`examples/erc20`](examples/erc20) watches public USDC deployments on two
  chains and filters `Transfer` and `Approval` by indexed addresses.
- [`examples/uniswap-v3`](examples/uniswap-v3) watches a public factory and a
  configured pool as separate sources.
- [`examples/uniswap-v4`](examples/uniswap-v4) watches the singleton PoolManager
  and filters `Swap` by indexed pool ID.
- [`examples/ctf`](examples/ctf) watches public Polymarket exchanges and routes
  decoded `OrderFilled` events to a custom handler.

Copy `.env.example` to `.env`, replace placeholders, then run an example:

```sh
go run ./examples/ctf -config examples/ctf/fasura.yaml
```

Each example keeps its ABI files beside its YAML and application.

## Planned extensions

- dynamic `AddAddresses` and `RemoveAddresses` runtime API;
- discovery plugins backed by factory events, custom code, or external indexers;
- WebSocket, SSE, and gRPC output plugins with explicit slow-client policies;
- reconnect gap recovery and checkpoints;
- reorg buffering and finalized block watermarks.
