package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port        string
	DatabaseURL string
	// postgress ne gleda ima jer ga vec sadrdzi u uriju
	DBName            string
	WalletAddress     string
	RPCURL            string
	TokenContract     string
	TokenDecimals     int
	AdminPassword     string
	DiscordWebhookURL string
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
		Port:              port,
		DatabaseURL:       dbURL,
		DBName:            os.Getenv("SHOP_DB_NAME"),
		WalletAddress:     os.Getenv("WALLET_ADDRESS"),
		RPCURL:            EnvOr("SEPOLIA_RPC_URL", "https://ethereum-sepolia-rpc.publicnode.com"),
		TokenContract:     EnvOr("USDT_CONTRACT", "0x74b0ef872a9f1a4bbb07a01a6b4376379737ff6f"),
		TokenDecimals:     6,
		AdminPassword:     os.Getenv("ADMIN_PASSWORD"),
		DiscordWebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
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

func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
