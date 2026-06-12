package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type order struct {
	ID          string  `json:"id" bson:"_id"`
	BuyerWallet string  `json:"buyer_wallet" binding:"required" bson:"buyer_wallet"`
	TxHash      *string `json:"tx_hash,omitempty" bson:"tx_hash"`
	// AmountUSDT is output-only: the store computes it from the item's stored
	// price × quantity at creation, so a crafted request can't understate what
	// the payment verifier expects on-chain. Any client-sent value is ignored.
	AmountUSDT   string    `json:"amount_usdt" bson:"amount_usdt"`
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

type attachTxRequest struct {
	TxHash string `json:"tx_hash" binding:"required"`
}

// attachOrderTx records the payment transaction for a pending, unpaid order.
// The storefront creates orders first (reserving stock), then pays, then
// attaches the resulting hash here for the background verifier to confirm.
// The conditional store update makes the attach one-shot: a settled or
// already-paid order cannot have its hash swapped.
func attachOrderTx(s Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in attachTxRequest
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := s.SetOrderTx(c.Request.Context(), c.Param("id"), in.TxHash)
		if errors.Is(err, errNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no pending unpaid order with that id"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
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
		out, err := s.CreateOrder(c.Request.Context(), in)
		switch {
		case errors.Is(err, errNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		case errors.Is(err, errInsufficientStock):
			c.JSON(http.StatusConflict, gin.H{"error": "not enough stock"})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusCreated, out)
		}
	}
}
