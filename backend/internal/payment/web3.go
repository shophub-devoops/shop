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

// potpis transfer eventa, kada se ERC 20 token potiise on emituje event, taj event ima topic
// koji je keccak256 hash tog potpisa, mi ga izracunamo jednom i onda trazimo bas taj hash u logovima
var transferSig = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

// znaci ovo iznad je otisak prsta transfer eventa

type paymentStatus string

const (
	statusPending   paymentStatus = "pending"
	statusConfirmed paymentStatus = "confirmed"
	statusFailed    paymentStatus = "failed"
)

type Verifier struct {
	client    *ethclient.Client // konekcija ka sepoliji
	token     common.Address    // adresa naseg TestUSDT token contracta
	recipient common.Address    // wallet prodavnice
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

func (v *Verifier) verify(ctx context.Context, txHash string, minAmount *big.Int) (paymentStatus, error) {
	// prvo dohvati potvrdu izvrsenja
	receipt, err := v.client.TransactionReceipt(ctx, common.HexToHash(txHash))
	if errors.Is(err, ethereum.NotFound) { // transakcija jos nije rudarena, probaj kasnije
		return statusPending, nil // znaci nije greska, samo cekamo da se skuva
	}
	if err != nil {
		return "", err
	}
	if receipt.Status != types.ReceiptStatusSuccessful { // transakcija propala, ali ima racun
		return statusFailed, nil // i propale transakcije imaju racun
	}
	if matchingTransfer(receipt.Logs, v.token, v.recipient, minAmount) {
		return statusConfirmed, nil // ako ima pravi transfer event onda je confirmed
	}
	return statusFailed, nil
}

// citanje event loga
func matchingTransfer(logs []*types.Log, token, recipient common.Address, minAmount *big.Int) bool {
	for _, lg := range logs { // da li je log emitovao nas token contract + da li ima
		// 3 topica + da li je prvi topic bas transfer signature
		if lg.Address != token || len(lg.Topics) != 3 || lg.Topics[0] != transferSig {
			continue // znaci lg.Address je adresa naseg token ugovora, nju mora da emituje NAS token
		}
		// Topics[1]=from, Topics[2]=to, topic 2 je nas wallet
		if common.BytesToAddress(lg.Topics[2].Bytes()) != recipient {
			continue
		}
		if new(big.Int).SetBytes(lg.Data).Cmp(minAmount) >= 0 {
			return true
		}
	}
	return false
}

// konvertujemo u cele brojeve jer blocchain radi se celim brojevima jer su oni precizniji
func ToBaseUnits(amount string, decimals int) (*big.Int, error) {
	r, ok := new(big.Rat).SetString(amount)
	if !ok {
		return nil, fmt.Errorf("invalid amount %q", amount)
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	r.Mul(r, new(big.Rat).SetInt(scale))
	return new(big.Int).Quo(r.Num(), r.Denom()), nil
}
