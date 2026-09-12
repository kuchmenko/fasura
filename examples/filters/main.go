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
	makerAddress := os.Getenv("FILTER_MAKER_ADDRESS")
	if !common.IsHexAddress(makerAddress) {
		log.Fatal("FILTER_MAKER_ADDRESS must be a valid address")
	}

	contract := common.HexToAddress("0x4bfb41d5b3570defd03c39a9a4d8de6bd8b8982e")
	maker := common.HexToAddress(makerAddress)
	orderFilled := crypto.Keccak256Hash([]byte(
		"OrderFilled(bytes32,address,address,uint256,uint256,uint256,uint256,uint256)",
	))

	config := fasura.Config{Chains: []fasura.Chain{{
		ID:     137,
		RPCURL: rpcURL,
		Streams: []fasura.Stream{{
			Name: "maker-order-filled",
			Query: ethereum.FilterQuery{
				Addresses: []common.Address{contract},
				Topics: [][]common.Hash{
					{orderFilled},
					nil, // any indexed orderHash
					{common.BytesToHash(maker.Bytes())},
				},
			},
		}},
	}}}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := fasura.Run(ctx, config, func(_ context.Context, event fasura.Event) error {
		fmt.Printf("maker=%s tx=%s index=%d\n", common.BytesToAddress(event.Log.Topics[2].Bytes()), event.Log.TxHash, event.Log.Index)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
