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
	baseRPC := os.Getenv("BASE_WS_RPC_URL")
	polygonRPC := os.Getenv("POLYGON_WS_RPC_URL")
	if baseRPC == "" || polygonRPC == "" {
		log.Fatal("BASE_WS_RPC_URL and POLYGON_WS_RPC_URL are required")
	}

	transfer := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	config := fasura.Config{Chains: []fasura.Chain{
		{
			ID:     8453,
			RPCURL: baseRPC,
			Streams: []fasura.Stream{{
				Name: "transfers",
				Query: ethereum.FilterQuery{
					Addresses: []common.Address{common.HexToAddress("0x4200000000000000000000000000000000000006")},
					Topics:    [][]common.Hash{{transfer}},
				},
			}},
		},
		{
			ID:     137,
			RPCURL: polygonRPC,
			Streams: []fasura.Stream{{
				Name: "transfers",
				Query: ethereum.FilterQuery{
					Addresses: []common.Address{common.HexToAddress("0x7ceB23fD6bC0adD59E62ac25578270cFf1b9f619")},
					Topics:    [][]common.Hash{{transfer}},
				},
			}},
		},
	}}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := fasura.Run(ctx, config, func(_ context.Context, event fasura.Event) error {
		fmt.Printf("chain=%d stream=%s block=%d tx=%s\n", event.ChainID, event.Stream, event.Log.BlockNumber, event.Log.TxHash)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
