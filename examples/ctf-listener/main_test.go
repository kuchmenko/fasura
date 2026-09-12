package main

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
)

func TestParseOrderFilledV2DecodesIndexedAndDataFields(t *testing.T) {
	t.Parallel()

	contractABI, err := loadABI("../../abis/CTFExchangeV2.json")
	if err != nil {
		t.Fatal(err)
	}
	definition := contractABI.Events["OrderFilled"]
	contractAddress := common.HexToAddress("0x1234")
	maker := common.HexToAddress("0x5678")
	taker := common.HexToAddress("0x9abc")
	orderHash := [32]byte{1, 2, 3}
	builder := [32]byte{4, 5, 6}
	metadata := [32]byte{7, 8, 9}
	data, err := definition.Inputs.NonIndexed().Pack(
		uint8(1),
		big.NewInt(17),
		big.NewInt(23),
		big.NewInt(31),
		big.NewInt(43),
		builder,
		metadata,
	)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := parseOrderFilled(8453, map[common.Address]contract{
		contractAddress: {
			Source:          "POLYMARKET",
			ContractVersion: "V2",
			ExchangeType:    "STANDARD",
			ABI:             contractABI,
		},
	}, gethtypes.Log{
		Address: contractAddress,
		Topics: []common.Hash{
			definition.ID,
			orderHash,
			common.BytesToHash(maker.Bytes()),
			common.BytesToHash(taker.Bytes()),
		},
		Data:        data,
		BlockNumber: 101,
		TxHash:      common.HexToHash("0xdef0"),
		Index:       3,
	})
	if err != nil {
		t.Fatal(err)
	}

	if parsed.ChainID != 8453 || parsed.Contract != contractAddress || parsed.Maker != maker || parsed.Taker != taker {
		t.Fatalf("parsed identity fields = %+v", parsed)
	}
	if parsed.Side == nil || *parsed.Side != 1 {
		t.Fatalf("side = %v, want 1", parsed.Side)
	}
	if parsed.TokenID.Cmp(big.NewInt(17)) != 0 || parsed.MakerAmountFilled.Cmp(big.NewInt(23)) != 0 || parsed.TakerAmountFilled.Cmp(big.NewInt(31)) != 0 || parsed.Fee.Cmp(big.NewInt(43)) != 0 {
		t.Fatalf("parsed numeric fields = %+v", parsed)
	}
	if parsed.Builder != common.Hash(builder) || parsed.Metadata != common.Hash(metadata) {
		t.Fatalf("builder/metadata = (%s, %s)", parsed.Builder, parsed.Metadata)
	}
}
