package main

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type item struct {
	ID    string `json:"id" bson:"_id"`
	Name  string `json:"name" binding:"required" bson:"name"`
	Price string `json:"price_usdt" binding:"required" bson:"price_usdt"`
	Stock int    `json:"stock" bson:"stock"`
}

func listItems(s Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := s.ListItems(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func createItem(s Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in item
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if in.ID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
			return
		}
		if err := s.CreateItem(c.Request.Context(), in); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, in)
	}
}

func updateItem(s Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var in item
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := s.UpdateItem(c.Request.Context(), id, in)
		if errors.Is(err, errNotFound) {
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

func deleteItem(s Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := s.DeleteItem(c.Request.Context(), c.Param("id"))
		if errors.Is(err, errNotFound) {
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
