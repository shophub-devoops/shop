package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type order struct {
	ID           string    `json:"id"`
	BuyerWallet  string    `json:"buyer_wallet" binding:"required"`
	TxHash       *string   `json:"tx_hash,omitempty"`
	AmountUSDT   string    `json:"amount_usdt" binding:"required"`
	CreatedAt    time.Time `json:"created_at"`
	ItemID       string    `json:"item_id,omitempty" binding:"required"`
	ItemQuantity int       `json:"item_quantity,omitempty" binding:"required,min=1"`
}

func listOrders(pool *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := pool.Query(c.Request.Context(),
			`SELECT id, buyer_wallet, tx_hash, amount_usdt::text, created_at
			 FROM orders ORDER BY created_at DESC`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var out []order
		for rows.Next() {
			var o order
			if err := rows.Scan(&o.ID, &o.BuyerWallet, &o.TxHash, &o.AmountUSDT, &o.CreatedAt); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			out = append(out, o)
		}
		if out == nil {
			out = []order{}
		}
		c.JSON(http.StatusOK, out)
	}
}

// createOrder records a pending order. tx_hash may be empty here; Web3 payment
// verification (D12) will fill it in via a follow-up endpoint once MetaMask
// returns a hash to the frontend.
func createOrder(pool *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in order
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if in.ID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
			return
		}

		stock, err := fetchItemStock(c.Request.Context(), pool, in.ItemID)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if stock < in.ItemQuantity {
			c.JSON(http.StatusConflict, gin.H{"error": "not enough stock"})
			return
		}

		_, err = pool.Exec(c.Request.Context(),
			`INSERT INTO orders (id, buyer_wallet, tx_hash, amount_usdt)
			 VALUES ($1, $2, $3, $4::numeric)`,
			in.ID, in.BuyerWallet, in.TxHash, in.AmountUSDT)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, in)
	}
}
