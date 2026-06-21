package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/shophub-devoops/shop/backend/internal/store"
)

func listOrders(s store.Store) gin.HandlerFunc {
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
func getOrder(s store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		o, err := s.GetOrder(c.Request.Context(), c.Param("id"))
		if errors.Is(err, store.ErrNotFound) {
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
func attachOrderTx(s store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in attachTxRequest
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := s.SetOrderTx(c.Request.Context(), c.Param("id"), in.TxHash)
		if errors.Is(err, store.ErrNotFound) {
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
func createOrder(s store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in store.Order
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
		case errors.Is(err, store.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		case errors.Is(err, store.ErrInsufficientStock):
			c.JSON(http.StatusConflict, gin.H{"error": "not enough stock"})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusCreated, out)
		}
	}
}
