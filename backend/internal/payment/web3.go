// Package payment confirms ERC-20 (USDT) payments to a Shop's wallet on Sepolia
// and drives the order-settlement sweep that reserves/releases stock.
package payment

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/shophub-devoops/shop/backend/internal/config"
)

// transferSig is the ERC-20 Transfer(address,address,uint256) event topic.
var transferSig = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

type paymentStatus string

const (
	statusPending   paymentStatus = "pending"
	statusConfirmed paymentStatus = "confirmed"
	statusFailed    paymentStatus = "failed"
)

// Verifier confirms an ERC-20 payment to the shop's wallet on Sepolia.
type Verifier struct {
	client    *ethclient.Client
	token     common.Address
	recipient common.Address
}

func NewVerifier(cfg config.Config) (*Verifier, error) {
	if cfg.WalletAddress == "" {
		return nil, fmt.Errorf("WALLET_ADDRESS not set")
	}
	client, err := ethclient.Dial(cfg.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", cfg.RPCURL, err)
	}
	return &Verifier{
		client:    client,
		token:     common.HexToAddress(cfg.TokenContract),
		recipient: common.HexToAddress(cfg.WalletAddress),
	}, nil
}

// verify inspects txHash's receipt and reports whether it carries an ERC-20
// Transfer of at least minAmount (base units) to the shop wallet. A tx that is
// not yet mined returns statusPending with no error so the caller can retry.
func (v *Verifier) verify(ctx context.Context, txHash string, minAmount *big.Int) (paymentStatus, error) {
	receipt, err := v.client.TransactionReceipt(ctx, common.HexToHash(txHash))
	if errors.Is(err, ethereum.NotFound) {
		return statusPending, nil
	}
	if err != nil {
		return "", err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return statusFailed, nil
	}
	if matchingTransfer(receipt.Logs, v.token, v.recipient, minAmount) {
		return statusConfirmed, nil
	}
	return statusFailed, nil
}

// matchingTransfer reports whether logs contain a Transfer emitted by token that
// sends at least minAmount to recipient.
func matchingTransfer(logs []*types.Log, token, recipient common.Address, minAmount *big.Int) bool {
	for _, lg := range logs {
		if lg.Address != token || len(lg.Topics) != 3 || lg.Topics[0] != transferSig {
			continue
		}
		// Topics[1]=from, Topics[2]=to (both left-padded to 32 bytes); Data=value.
		if common.BytesToAddress(lg.Topics[2].Bytes()) != recipient {
			continue
		}
		if new(big.Int).SetBytes(lg.Data).Cmp(minAmount) >= 0 {
			return true
		}
	}
	return false
}

// ToBaseUnits converts a decimal token amount ("5", "5.5") to integer base units
// for the given decimals (5.5 @ 6 decimals -> 5500000). Sub-unit precision is
// floored away.
func ToBaseUnits(amount string, decimals int) (*big.Int, error) {
	r, ok := new(big.Rat).SetString(amount)
	if !ok {
		return nil, fmt.Errorf("invalid amount %q", amount)
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	r.Mul(r, new(big.Rat).SetInt(scale))
	return new(big.Int).Quo(r.Num(), r.Denom()), nil
}
