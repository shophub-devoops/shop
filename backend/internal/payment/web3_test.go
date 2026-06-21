package payment

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	tToken = common.HexToAddress("0x74b0ef872a9f1a4bbb07a01a6b4376379737ff6f")
	tShop  = common.HexToAddress("0x3a6B1512a8ccF0315c0A392E98975Ac659D24e06")
	tBuyer = common.HexToAddress("0x45bE1B0D6863Da84cc27A271bFbfE7f8dd060ac7")
)

// transferLog builds an ERC-20 Transfer log from->to of value, emitted by token.
func transferLog(token, from, to common.Address, value *big.Int) *types.Log {
	return &types.Log{
		Address: token,
		Topics: []common.Hash{
			transferSig,
			common.BytesToHash(from.Bytes()),
			common.BytesToHash(to.Bytes()),
		},
		Data: common.LeftPadBytes(value.Bytes(), 32),
	}
}

func TestMatchingTransfer(t *testing.T) {
	five := big.NewInt(5_000_000) // 5 USDT @ 6 decimals
	other := common.HexToAddress("0x000000000000000000000000000000000000dEaD")

	cases := []struct {
		name string
		logs []*types.Log
		min  *big.Int
		want bool
	}{
		{"exact amount to shop", []*types.Log{transferLog(tToken, tBuyer, tShop, five)}, five, true},
		{"more than enough", []*types.Log{transferLog(tToken, tBuyer, tShop, big.NewInt(6_000_000))}, five, true},
		{"too little", []*types.Log{transferLog(tToken, tBuyer, tShop, big.NewInt(4_999_999))}, five, false},
		{"wrong recipient", []*types.Log{transferLog(tToken, tBuyer, other, five)}, five, false},
		{"wrong token", []*types.Log{transferLog(other, tBuyer, tShop, five)}, five, false},
		{"no logs", nil, five, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchingTransfer(tc.logs, tToken, tShop, tc.min); got != tc.want {
				t.Errorf("matchingTransfer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestToBaseUnits(t *testing.T) {
	cases := []struct {
		amount string
		want   int64
	}{
		{"5", 5_000_000},
		{"5.5", 5_500_000},
		{"0.000001", 1},
		{"123.456789", 123_456_789},
	}
	for _, tc := range cases {
		got, err := ToBaseUnits(tc.amount, 6)
		if err != nil {
			t.Fatalf("ToBaseUnits(%q): %v", tc.amount, err)
		}
		if got.Int64() != tc.want {
			t.Errorf("ToBaseUnits(%q) = %d, want %d", tc.amount, got.Int64(), tc.want)
		}
	}
	if _, err := ToBaseUnits("not-a-number", 6); err == nil {
		t.Error("expected error for invalid amount")
	}
}
