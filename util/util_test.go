package util

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/WWaynee/content-hub/config"
)

func TestHashAndVerifyPassword(t *testing.T) {
	pwd := "Passw0rd@2026"
	hash, err := HashPassword(pwd)
	if err != nil {
		t.Fatalf("HashPassword 失败: %v", err)
	}
	if hash == pwd {
		t.Fatal("哈希不应等于明文")
	}
	if !VerifyPassword(pwd, hash) {
		t.Fatal("正确密码应校验通过")
	}
	if VerifyPassword("wrong", hash) {
		t.Fatal("错误密码应校验失败")
	}
}

func TestPasswordHashIsSalted(t *testing.T) {
	h1, _ := HashPassword("same-password")
	h2, _ := HashPassword("same-password")
	if h1 == h2 {
		t.Fatal("bcrypt 应按随机盐产生不同哈希")
	}
}

func assertSecretLoaded(t *testing.T) string {
	t.Helper()
	if _, err := config.Load(); err != nil {
		t.Skipf("加载配置失败，跳过: %v", err)
	}
	secret := config.Get().JWT.Secret
	if len(secret) < 16 {
		t.Skip("JWT_SECRET 未配置足够长度，跳过")
	}
	return secret
}

func TestGenerateAndParseToken(t *testing.T) {
	_ = assertSecretLoaded(t)
	token, err := GenerateToken(42, 7, "admin")
	if err != nil {
		t.Fatalf("GenerateToken 失败: %v", err)
	}
	claims, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken 失败: %v", err)
	}
	if claims.UserID != 42 || claims.TenantID != 7 || claims.Role != "admin" {
		t.Fatalf("claims 内容不符: %+v", claims)
	}
}

func TestParseInvalidToken(t *testing.T) {
	if _, err := ParseToken("not-a-jwt"); err == nil {
		t.Fatal("非法 token 应解析失败")
	}
}

// TestParseExpiredToken 构造一个已过期的 token，应解析失败。
func TestParseExpiredToken(t *testing.T) {
	secret := assertSecretLoaded(t)
	claims := &Claims{
		UserID:   1,
		TenantID: 1,
		Role:     "member",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // 已过期
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("构造过期 token 失败: %v", err)
	}
	if _, err := ParseToken(s); err == nil {
		t.Fatal("过期的 token 应解析失败")
	}
}
