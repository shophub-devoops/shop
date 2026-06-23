package httpapi

import (
	"errors"
	"math/big"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/shophub-devoops/shop/backend/internal/store"
)

// validateItem rejects negative price or stock. Returns a human-readable
// message (empty when valid) so the handler can answer 400 with it.
func validateItem(it store.Item) string {
	if it.Stock < 0 {
		return "stock cannot be negative"
	}
	r, ok := new(big.Rat).SetString(it.Price)
	if !ok {
		return "price must be a number"
	}
	if r.Sign() < 0 {
		return "price cannot be negative"
	}
	return ""
}

func listItems(s store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := s.ListItems(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func createItem(s store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in store.Item
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if in.ID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
			return
		}
		if msg := validateItem(in); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		err := s.CreateItem(c.Request.Context(), in)
		if errors.Is(err, store.ErrAlreadyExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "an item with this id already exists"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, in)
	}
}

func updateItem(s store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var in store.Item
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if msg := validateItem(in); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		err := s.UpdateItem(c.Request.Context(), id, in)
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		in.ID = id
		c.JSON(http.StatusOK, in)
	}
}

func deleteItem(s store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := s.DeleteItem(c.Request.Context(), c.Param("id"))
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}
