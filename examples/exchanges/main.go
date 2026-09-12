package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/kuchmenko/fasura"
)

func main() {
	rpcURL := os.Getenv("POLYGON_WS_RPC_URL")
	if rpcURL == "" {
		log.Fatal("POLYGON_WS_RPC_URL is required")
	}

	config := fasura.Config{Chains: []fasura.Chain{{
		ID:     137,
		RPCURL: rpcURL,
		Streams: []fasura.Stream{
			{
				Name: "polymarket-v1-fills",
				Query: ethereum.FilterQuery{
					Addresses: []common.Address{common.HexToAddress("0x4bfb41d5b3570defd03c39a9a4d8de6bd8b8982e")},
					Topics: [][]common.Hash{{crypto.Keccak256Hash([]byte(
						"OrderFilled(bytes32,address,address,uint256,uint256,uint256,uint256,uint256)",
					))}},
				},
			},
			{
				Name: "polymarket-v2-fills",
				Query: ethereum.FilterQuery{
					Addresses: []common.Address{common.HexToAddress("0xe111180000d2663c0091e4f400237545b87b996b")},
					Topics: [][]common.Hash{{crypto.Keccak256Hash([]byte(
						"OrderFilled(bytes32,address,address,uint8,uint256,uint256,uint256,uint256,bytes32,bytes32)",
					))}},
				},
			},
		},
	}}}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := fasura.Run(ctx, config, func(_ context.Context, event fasura.Event) error {
		fmt.Printf("%s contract=%s tx=%s index=%d removed=%t\n", event.Stream, event.Log.Address, event.Log.TxHash, event.Log.Index, event.Log.Removed)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
