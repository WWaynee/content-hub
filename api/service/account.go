package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/observability"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
	"github.com/WWaynee/content-hub/util"
)

// 账号业务层。

var (
	ErrTenantExists   = errors.New("租户已存在")
	ErrTenantNotFound = errors.New("租户不存在")
	ErrUsernameExists = errors.New("用户名已存在")
	ErrAccountInvalid = errors.New("用户名或密码错误")
)

// RegisterTenant 注册租户：原子创建 租户 + 首个管理员（role=admin）。
// 租户名全局唯一；管理员用户名全局唯一（驱动"登录不传租户ID"）。
func RegisterTenant(ctx context.Context, name, adminUsername, adminPassword string) (*model.User, string, error) {
	ex, err := storage.IsTenantNameExists(name)
	if err != nil {
		return nil, "", err
	}
	if ex {
		return nil, "", ErrTenantExists
	}
	uEx, err := storage.IsUsernameExistsGlobal(adminUsername)
	if err != nil {
		return nil, "", err
	}
	if uEx {
		return nil, "", ErrUsernameExists
	}
	t := &model.Tenant{Name: name, Status: 1}
	u := &model.User{Username: adminUsername, Role: storage.RoleAdmin, Status: 1}
	u.PasswordHash, _ = util.HashPassword(adminPassword)

	err = storage.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := storage.CreateTenant(tx, t); err != nil {
			return err
		}
		u.TenantID = t.ID
		return storage.CreateUser(tx, u)
	})
	if err != nil {
		return nil, "", err
	}
	token, err := util.GenerateToken(u.ID, u.TenantID, u.Role)
	if err != nil {
		return nil, "", err
	}
	auditCtx := observability.WithTenantUser(ctx, u.TenantID, u.ID)
	observability.WithContext(auditCtx).Info("注册租户",
		map[string]interface{}{"operation": "register_tenant", "tenant_id": u.TenantID})
	RecordAudit(auditCtx, "register_tenant", "注册租户: "+name)
	return u, token, nil
}

// Login 登录：按全局唯一用户名+密码校验（不传租户ID），返回 token。
func Login(ctx context.Context, username, password string) (*model.User, string, error) {
	u, err := storage.GetUserByUsernameGlobal(username)
	if err != nil || u.Status != 1 {
		return nil, "", ErrAccountInvalid
	}
	// 校验所属租户已启用
	t, err := storage.GetTenantByID(u.TenantID)
	if err != nil || t.Status != 1 {
		return nil, "", ErrAccountInvalid
	}
	if !util.VerifyPassword(password, u.PasswordHash) {
		return nil, "", ErrAccountInvalid
	}
	token, err := util.GenerateToken(u.ID, u.TenantID, u.Role)
	if err != nil {
		return nil, "", err
	}
	auditCtx := observability.WithTenantUser(ctx, u.TenantID, u.ID)
	observability.WithContext(auditCtx).Info("用户登录",
		map[string]interface{}{"operation": "login"})
	RecordAudit(auditCtx, "login", "用户登录: "+username)
	return u, token, nil
}

// RegisterMember 在指定租户内注册工作人员（默认 member）。
func RegisterMember(ctx context.Context, tenantID uint64, username, password string) (*model.User, error) {
	t, err := storage.GetTenantByID(tenantID)
	if err != nil || t.Status != 1 {
		return nil, ErrTenantNotFound
	}
	// 用户名全局唯一
	ex, err := storage.IsUsernameExistsGlobal(username)
	if err != nil {
		return nil, err
	}
	if ex {
		return nil, ErrUsernameExists
	}
	u := &model.User{TenantID: tenantID, Username: username, Role: storage.RoleMember, Status: 1}
	u.PasswordHash, _ = util.HashPassword(password)
	if err := storage.CreateUser(storage.GetDB(), u); err != nil {
		return nil, err
	}
	auditCtx := observability.WithTenantUser(ctx, tenantID, u.ID)
	observability.WithContext(auditCtx).Info("注册工作人员",
		map[string]interface{}{"operation": "register_member"})
	RecordAudit(auditCtx, "register_member", "注册工作人员: "+username)
	return u, nil
}
