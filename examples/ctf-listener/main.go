package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum"
	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/kuchmenko/fasura"
	"github.com/pelletier/go-toml/v2"
)

type config struct {
	Chains map[string]chainConfig `toml:"chains"`
}

type chainConfig struct {
	ChainID   uint64           `toml:"chain_id"`
	RPCURLEnv string           `toml:"rpc_url_env"`
	Wallets   []string         `toml:"wallets"`
	Contracts []contractConfig `toml:"contracts"`
}

type contractConfig struct {
	Address         string `toml:"address"`
	Source          string `toml:"source"`
	ContractVersion string `toml:"contract_version"`
	ExchangeType    string `toml:"exchange_type"`
	ABI             string `toml:"abi"`
}

type contract struct {
	Source          string
	ContractVersion string
	ExchangeType    string
	ABI             gethabi.ABI
}

type orderFilled struct {
	ChainID         uint64
	Source          string
	ContractVersion string
	ExchangeType    string
	Contract        common.Address
	BlockNumber     uint64
	TransactionHash common.Hash
	LogIndex        uint
	Removed         bool

	OrderHash         common.Hash
	Maker             common.Address
	Taker             common.Address
	MakerAssetID      *big.Int
	TakerAssetID      *big.Int
	Side              *uint8
	TokenID           *big.Int
	MakerAmountFilled *big.Int
	TakerAmountFilled *big.Int
	Fee               *big.Int
	Builder           common.Hash
	Metadata          common.Hash
}

func main() {
	configPath := flag.String("config", "fasura.toml", "path to the CTF listener config")
	flag.Parse()

	appConfig, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	fasuraConfig, routes, err := build(appConfig)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := fasura.Run(ctx, fasuraConfig, routes.Handle); err != nil {
		log.Printf("listener failed: %v", err)
	}
}

func build(appConfig config) (fasura.Config, fasura.Route, error) {
	result := fasura.Config{Chains: make([]fasura.Chain, 0, len(appConfig.Chains))}
	routes := make(fasura.Route, len(appConfig.Chains))

	for name, chainConfig := range appConfig.Chains {
		rpcURL := os.Getenv(chainConfig.RPCURLEnv)
		if rpcURL == "" {
			return fasura.Config{}, nil, fmt.Errorf("%s is required", chainConfig.RPCURLEnv)
		}

		addresses := make([]common.Address, 0, len(chainConfig.Contracts))
		eventTopics := make([]common.Hash, 0, len(chainConfig.Contracts))
		contracts := make(map[common.Address]contract, len(chainConfig.Contracts))
		for _, contractConfig := range chainConfig.Contracts {
			if !common.IsHexAddress(contractConfig.Address) {
				return fasura.Config{}, nil, fmt.Errorf("invalid contract address %q", contractConfig.Address)
			}

			contractABI, err := loadABI(contractConfig.ABI)
			if err != nil {
				return fasura.Config{}, nil, err
			}
			orderFilled, ok := contractABI.Events["OrderFilled"]
			if !ok {
				return fasura.Config{}, nil, fmt.Errorf("OrderFilled event not found in %q", contractConfig.ABI)
			}

			address := common.HexToAddress(contractConfig.Address)
			addresses = append(addresses, address)
			eventTopics = append(eventTopics, orderFilled.ID)
			contracts[address] = contract{
				Source:          contractConfig.Source,
				ContractVersion: contractConfig.ContractVersion,
				ExchangeType:    contractConfig.ExchangeType,
				ABI:             contractABI,
			}
		}

		makerTopics := make([]common.Hash, 0, len(chainConfig.Wallets))
		for _, wallet := range chainConfig.Wallets {
			if !common.IsHexAddress(wallet) {
				return fasura.Config{}, nil, fmt.Errorf("invalid wallet address %q", wallet)
			}
			makerTopics = append(makerTopics, common.BytesToHash(common.HexToAddress(wallet).Bytes()))
		}

		topics := [][]common.Hash{eventTopics}
		if len(makerTopics) > 0 {
			// OrderFilled topic 2 is the indexed maker address.
			topics = append(topics, nil, makerTopics)
		}

		streamName := name + "-order-filled"
		result.Chains = append(result.Chains, fasura.Chain{
			ID:     chainConfig.ChainID,
			RPCURL: rpcURL,
			Streams: []fasura.Stream{{
				Name: streamName,
				Query: ethereum.FilterQuery{
					Addresses: addresses,
					Topics:    topics,
				},
			}},
		})

		chainID := chainConfig.ChainID
		routes[streamName] = func(_ context.Context, event fasura.Event) error {
			filled, err := parseOrderFilled(chainID, contracts, event.Log)
			if err != nil {
				return err
			}
			log.Printf("%s: %+v", event.Stream, filled)
			return nil
		}
	}

	return result, routes, nil
}

func loadConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	var result config
	if err := toml.Unmarshal(data, &result); err != nil {
		return config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	return result, nil
}

func loadABI(path string) (gethabi.ABI, error) {
	file, err := os.Open(path)
	if err != nil {
		return gethabi.ABI{}, fmt.Errorf("open ABI %q: %w", path, err)
	}
	defer file.Close()

	contractABI, err := gethabi.JSON(file)
	if err != nil {
		return gethabi.ABI{}, fmt.Errorf("parse ABI %q: %w", path, err)
	}
	return contractABI, nil
}

func parseOrderFilled(chainID uint64, contracts map[common.Address]contract, event gethtypes.Log) (orderFilled, error) {
	contract, ok := contracts[event.Address]
	if !ok {
		return orderFilled{}, fmt.Errorf("missing contract: %s", event.Address)
	}

	definition := contract.ABI.Events["OrderFilled"]
	if len(event.Topics) == 0 || event.Topics[0] != definition.ID {
		return orderFilled{}, fmt.Errorf("unexpected event topic for %s", event.Address)
	}

	indexed := make(gethabi.Arguments, 0, len(definition.Inputs))
	for _, input := range definition.Inputs {
		if input.Indexed {
			indexed = append(indexed, input)
		}
	}
	if len(event.Topics) != len(indexed)+1 {
		return orderFilled{}, fmt.Errorf("unexpected topic count: got %d, want %d", len(event.Topics), len(indexed)+1)
	}

	values := make(map[string]any)
	if err := gethabi.ParseTopicsIntoMap(values, indexed, event.Topics[1:]); err != nil {
		return orderFilled{}, fmt.Errorf("parse indexed values: %w", err)
	}
	if err := definition.Inputs.NonIndexed().UnpackIntoMap(values, event.Data); err != nil {
		return orderFilled{}, fmt.Errorf("parse data values: %w", err)
	}

	orderHash, ok := values["orderHash"].([32]byte)
	if !ok {
		return orderFilled{}, fmt.Errorf("unexpected orderHash type: %T", values["orderHash"])
	}
	maker, ok := values["maker"].(common.Address)
	if !ok {
		return orderFilled{}, fmt.Errorf("unexpected maker type: %T", values["maker"])
	}
	taker, ok := values["taker"].(common.Address)
	if !ok {
		return orderFilled{}, fmt.Errorf("unexpected taker type: %T", values["taker"])
	}

	result := orderFilled{
		ChainID:           chainID,
		Source:            contract.Source,
		ContractVersion:   contract.ContractVersion,
		ExchangeType:      contract.ExchangeType,
		Contract:          event.Address,
		BlockNumber:       event.BlockNumber,
		TransactionHash:   event.TxHash,
		LogIndex:          event.Index,
		Removed:           event.Removed,
		OrderHash:         orderHash,
		Maker:             maker,
		Taker:             taker,
		MakerAssetID:      bigInt(values["makerAssetId"]),
		TakerAssetID:      bigInt(values["takerAssetId"]),
		TokenID:           bigInt(values["tokenId"]),
		MakerAmountFilled: bigInt(values["makerAmountFilled"]),
		TakerAmountFilled: bigInt(values["takerAmountFilled"]),
		Fee:               bigInt(values["fee"]),
		Builder:           hash(values["builder"]),
		Metadata:          hash(values["metadata"]),
	}
	if side, ok := values["side"].(uint8); ok {
		result.Side = &side
	}

	return result, nil
}

func bigInt(value any) *big.Int {
	result, _ := value.(*big.Int)
	return result
}

func hash(value any) common.Hash {
	raw, _ := value.([32]byte)
	return raw
}
