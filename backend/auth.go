package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const adminTokenTTL = 24 * time.Hour

// adminAuth guards the shop-owner endpoints (item writes, order listing).
// The operator provisions a per-shop admin password Secret and injects it as
// ADMIN_PASSWORD; a successful login returns a JWT signed with a key derived
// from that password, so any replica can verify tokens without shared state.
// A nil adminAuth (ADMIN_PASSWORD unset — local dev, tests) disables the gate.
type adminAuth struct {
	password string
	key      []byte
}

func newAdminAuth(password string) *adminAuth {
	if password == "" {
		return nil
	}
	// Derive the JWT signing key from the password so no second secret needs
	// to be provisioned; the prefix domain-separates it from other HMAC uses.
	sum := sha256.Sum256([]byte("shop-admin-jwt:" + password))
	return &adminAuth{password: password, key: sum[:]}
}

type adminLoginRequest struct {
	Password string `json:"password" binding:"required"`
}

// adminLogin exchanges the shop's admin password for a bearer token. When auth
// is disabled (no ADMIN_PASSWORD) any password yields a dummy token so the
// admin UI flow still works in local dev.
func adminLogin(a *adminAuth) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in adminLoginRequest
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if a == nil {
			c.JSON(http.StatusOK, gin.H{"token": "dev-mode"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(in.Password), []byte(a.password)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
			return
		}
		t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
			Subject:   "admin",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(adminTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		})
		token, err := t.SignedString(a.key)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "sign token"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token})
	}
}

// require rejects requests without a valid admin bearer token. A nil receiver
// is a pass-through so local dev and the handler tests run without auth.
func (a *adminAuth) require() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a == nil {
			c.Next()
			return
		}
		raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return a.key, nil
		})
		if err != nil || !tok.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.Next()
	}
}
