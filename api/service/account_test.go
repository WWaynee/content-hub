package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestInit 连接真实 MySQL（读取项目根 .env）。未连上则跳过（供 CI）。
func testDB(t *testing.T) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("跳过：加载配置失败 %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("跳过：MySQL 不可用 %v", err)
	}
}

func TestRegisterTenantAndLogin(t *testing.T) {
	testDB(t)
	ctx := context.Background()

	// 用随机租户名避免与已有数据/并行冲突
	name := "unittest_" + randSuffix()
	u, token, err := RegisterTenant(ctx, name, "utadmin", "pass123456")
	if err != nil {
		t.Fatalf("RegisterTenant 失败: %v", err)
	}
	if u.Role != "admin" {
		t.Fatalf("首帐号应为 admin, 实际=%s", u.Role)
	}
	if token == "" {
		t.Fatal("应返回 token")
	}

	// 重复注册同租户应失败
	if _, _, err := RegisterTenant(ctx, name, "utadmin2", "pass123456"); err != ErrTenantExists {
		t.Fatalf("重复注册应返回 ErrTenantExists, 实际=%v", err)
	}

	// 正确登录
	lu, ltoken, err := Login(ctx, u.TenantID, "utadmin", "pass123456")
	if err != nil {
		t.Fatalf("Login 失败: %v", err)
	}
	if lu.ID != u.ID || ltoken == "" {
		t.Fatalf("登录结果不符")
	}

	// 错误密码
	if _, _, err := Login(ctx, u.TenantID, "utadmin", "wrong"); err != ErrAccountInvalid {
		t.Fatalf("错误密码应返回 ErrAccountInvalid, 实际=%v", err)
	}
	// 错误租户（不存在的租户）
	if _, _, err := Login(ctx, 99999999, "utadmin", "pass123456"); err != ErrAccountInvalid {
		t.Fatalf("不存在的租户应返回 ErrAccountInvalid, 实际=%v", err)
	}

	// 清理测试数据
	cleanup(ctx, u.TenantID)
}

func TestRegisterMemberRole(t *testing.T) {
	testDB(t)
	ctx := context.Background()

	name := "unittest_" + randSuffix()
	admin, _, err := RegisterTenant(ctx, name, "utadmin", "pass123456")
	if err != nil {
		t.Fatalf("RegisterTenant 失败: %v", err)
	}
	m, err := RegisterMember(ctx, admin.TenantID, "utworker", "pass123456")
	if err != nil {
		t.Fatalf("RegisterMember 失败: %v", err)
	}
	if m.Role != "member" {
		t.Fatalf("工作人员 role 应为 member, 实际=%s", m.Role)
	}
	// 重复用户名失败
	if _, err := RegisterMember(ctx, admin.TenantID, "utworker", "pass123456"); err != ErrUsernameExists {
		t.Fatalf("重复用户名应返回 ErrUsernameExists, 实际=%v", err)
	}
	cleanup(ctx, admin.TenantID)
}

// randSuffix 生成短随机串，用于测试租户名避免冲突。
func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// cleanup 删除测试租户，避免污染数据与后续重跑冲突。
func cleanup(ctx context.Context, tenantID uint64) {
	db := storage.GetDB()
	if db == nil {
		return
	}
	db.Exec("DELETE FROM audit_logs WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM users WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM tenants WHERE id = ?", tenantID)
}
