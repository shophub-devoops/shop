package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type item struct {
	ID    string `json:"id"`
	Name  string `json:"name" binding:"required"`
	Price string `json:"price_usdt" binding:"required"`
	Stock int    `json:"stock"`
}

func listItems(pool *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := pool.Query(c.Request.Context(),
			`SELECT id, name, price_usdt::text, stock FROM items ORDER BY name`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var out []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.ID, &it.Name, &it.Price, &it.Stock); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			out = append(out, it)
		}
		if out == nil {
			out = []item{}
		}
		c.JSON(http.StatusOK, out)
	}
}

func createItem(pool *pgxpool.Pool) gin.HandlerFunc {
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
		_, err := pool.Exec(c.Request.Context(),
			`INSERT INTO items (id, name, price_usdt, stock) VALUES ($1, $2, $3::numeric, $4)`,
			in.ID, in.Name, in.Price, in.Stock)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, in)
	}
}

func updateItem(pool *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var in item
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		tag, err := pool.Exec(c.Request.Context(),
			`UPDATE items SET name = $1, price_usdt = $2::numeric, stock = $3 WHERE id = $4`,
			in.Name, in.Price, in.Stock, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if tag.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
			return
		}
		in.ID = id
		c.JSON(http.StatusOK, in)
	}
}

func deleteItem(pool *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		tag, err := pool.Exec(c.Request.Context(),
			`DELETE FROM items WHERE id = $1`, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if tag.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// fetchItemStock loads the current stock for an item; returns pgx.ErrNoRows if
// the item does not exist. createOrder uses this to ensure the buyer is not
// reserving more than is on the shelf.
func fetchItemStock(ctx context.Context, pool *pgxpool.Pool, id string) (int, error) {
	var stock int
	err := pool.QueryRow(ctx, `SELECT stock FROM items WHERE id = $1`, id).Scan(&stock)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	return stock, err
}
