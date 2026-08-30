package util

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/WWaynee/content-hub/config"
)

// Claims JWT 载荷（最小鉴权信息，不存可变/展示型字段）
type Claims struct {
	UserID   uint64 `json:"user_id"`
	TenantID uint64 `json:"tenant_id"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// GenerateToken 签发 JWT。
func GenerateToken(userID, tenantID uint64, role string) (string, error) {
	expire := time.Duration(config.Get().JWT.ExpireSeconds) * time.Second
	claims := &Claims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expire)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(config.Get().JWT.Secret))
}

// ParseToken 解析并校验 JWT，返回 Claims。
func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},
		func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("签名算法不匹配")
			}
			return []byte(config.Get().JWT.Secret), nil
		},
	)
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("无效的 token")
	}
	return claims, nil
}
