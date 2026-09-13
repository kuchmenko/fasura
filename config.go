package fasura

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Config describes chains and contract sources handled by one App.
type Config struct {
	Chains  map[string]Chain
	Sources []Source
}

// Chain describes one EVM RPC connection.
type Chain struct {
	ID     uint64
	RPCURL string
}

// Source groups deployments and events that share one ABI.
type Source struct {
	Name        string
	ABI         abi.ABI
	Deployments map[string]Deployment
	Events      []EventConfig
}

// Deployment lists contract addresses for one configured chain.
type Deployment struct {
	Addresses []common.Address
}

// EventConfig selects one ABI event and optional indexed argument matches.
type EventConfig struct {
	Name  string
	Match []Match
}

// Match is one AND clause. Values for one argument are OR alternatives.
// Multiple Match values on EventConfig are OR alternatives.
type Match map[string][]any
