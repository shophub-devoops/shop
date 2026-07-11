package payment

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/shophub-devoops/shop/backend/internal/config"
)

// sta sve treba frontendu za erc 20 transfer, da bi saznao kuda da posalje pare, sa kog tokena
// na koliko decimala i koja mreza, sve dolazi iz configa koji dolazi iz enva kog je operator injektovao
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
