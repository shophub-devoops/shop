package payment

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/shophub-devoops/shop/backend/internal/config"
)

// ShopInfo exposes the on-chain payment parameters the frontend needs to build
// the ERC-20 transfer.
func ShopInfo(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"wallet_address": cfg.WalletAddress,
			"token_contract": cfg.TokenContract,
			"token_decimals": cfg.TokenDecimals,
			"chain_id":       11155111, // Sepolia
		})
	}
}
