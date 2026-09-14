package fasura

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Event is one decoded EVM log tagged with its configured source and chain.
type Event struct {
	Source  string
	Name    string
	Chain   string
	ChainID uint64
	Address common.Address
	Values  map[string]any
	Log     types.Log
}

// Key returns the stable source.event handler key.
func (event Event) Key() string {
	return event.Source + "." + event.Name
}

// Handler processes one decoded event.
type Handler func(context.Context, Event) error
