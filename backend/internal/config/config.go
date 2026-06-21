// Package config loads the Shop backend's runtime configuration from the
// environment. The operator injects most of these (DATABASE_URL, WALLET_ADDRESS,
// ADMIN_PASSWORD, …); the rest default to the project's Sepolia test setup.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port        string
	DatabaseURL string
	// DBName is the database the Mongo client uses (operator injects SHOP_DB_NAME
	// = shop name). Ignored by Postgres, whose URI already names the database.
	DBName string

	// Web3 payment (D12). WalletAddress is this shop's on-chain recipient — the
	// operator injects it from Shop.spec.walletAddress. Token/RPC default to the
	// project's Sepolia test setup and can be overridden by env.
	WalletAddress string
	RPCURL        string
	TokenContract string
	TokenDecimals int

	// AdminPassword guards item writes and order listing. Injected by the
	// operator from the per-shop admin Secret; empty disables the gate (dev).
	AdminPassword string
}

func Load() (Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	cfg := Config{
		Port:          port,
		DatabaseURL:   dbURL,
		DBName:        os.Getenv("SHOP_DB_NAME"),
		WalletAddress: os.Getenv("WALLET_ADDRESS"),
		RPCURL:        EnvOr("SEPOLIA_RPC_URL", "https://ethereum-sepolia-rpc.publicnode.com"),
		TokenContract: EnvOr("USDT_CONTRACT", "0x74b0ef872a9f1a4bbb07a01a6b4376379737ff6f"),
		TokenDecimals: 6,
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
	}
	if d := os.Getenv("USDT_DECIMALS"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil {
			return Config{}, fmt.Errorf("USDT_DECIMALS: %w", err)
		}
		cfg.TokenDecimals = n
	}
	return cfg, nil
}

// EnvOr returns the value of key, or fallback when it is unset/empty.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
