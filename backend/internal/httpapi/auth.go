package httpapi

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

// jwt kljuc za potpis se izvede iz admin lozinke, lozinka je i identitet i osnova za potpis tokena
type adminAuth struct {
	password string
	key      []byte
}

func NewAdminAuth(password string) *adminAuth {
	if password == "" { // no-op pattern
		return nil
	}
	// vadi kljuc iz lozinke
	sum := sha256.Sum256([]byte("shop-admin-jwt:" + password))
	return &adminAuth{password: password, key: sum[:]}
}

type adminLoginRequest struct {
	Password string `json:"password" binding:"required"`
}

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
		// constant time uvek traje isto, ne dozvoljava hakeru da na osnovu vremenu odgovora
		// skonta koji karakter je netacan u passwordu i da ga probije na brute force
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

func (a *adminAuth) require() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a == nil {
			c.Next() // gate je iskljucen
			return
		}
		raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) { // proveri potpis
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
