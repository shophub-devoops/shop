package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type order struct {
	ID           string    `json:"id" bson:"_id"`
	BuyerWallet  string    `json:"buyer_wallet" binding:"required" bson:"buyer_wallet"`
	TxHash       *string   `json:"tx_hash,omitempty" bson:"tx_hash"`
	AmountUSDT   string    `json:"amount_usdt" binding:"required" bson:"amount_usdt"`
	Status       string    `json:"status" bson:"status"`
	CreatedAt    time.Time `json:"created_at" bson:"created_at"`
	ItemID       string    `json:"item_id,omitempty" binding:"required" bson:"item_id"`
	ItemQuantity int       `json:"item_quantity,omitempty" binding:"required,min=1" bson:"item_quantity"`
}

func listOrders(s Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := s.ListOrders(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

// getOrder returns a single order so the frontend can poll its payment status
// (pending -> confirmed/failed).
func getOrder(s Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		o, err := s.GetOrder(c.Request.Context(), c.Param("id"))
		if errors.Is(err, errNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, o)
	}
}

// createOrder records a pending order. tx_hash may be empty here; Web3 payment
// verification (D12) confirms it later via the background sweep once MetaMask
// returns a hash to the frontend.
func createOrder(s Store) gin.HandlerFunc {
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
		err := s.CreateOrder(c.Request.Context(), in)
		switch {
		case errors.Is(err, errNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		case errors.Is(err, errInsufficientStock):
			c.JSON(http.StatusConflict, gin.H{"error": "not enough stock"})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		default:
			in.Status = "pending"
			c.JSON(http.StatusCreated, in)
		}
	}
}
