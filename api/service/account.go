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
func RegisterTenant(ctx context.Context, name, adminUsername, adminPassword string) (*model.User, string, error) {
	ex, err := storage.IsTenantNameExists(name)
	if err != nil {
		return nil, "", err
	}
	if ex {
		return nil, "", ErrTenantExists
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
	observability.WithContext(ctx).Info("注册租户",
		map[string]interface{}{"operation": "register_tenant", "tenant_id": u.TenantID})
	return u, token, nil
}

// Login 登录：按租户+用户名+密码校验，返回 token。
func Login(ctx context.Context, tenantID uint64, username, password string) (*model.User, string, error) {
	t, err := storage.GetTenantByID(tenantID)
	if err != nil || t.Status != 1 {
		return nil, "", ErrAccountInvalid
	}
	u, err := storage.GetUserByUsername(tenantID, username)
	if err != nil || u.Status != 1 {
		return nil, "", ErrAccountInvalid
	}
	if !util.VerifyPassword(password, u.PasswordHash) {
		return nil, "", ErrAccountInvalid
	}
	token, err := util.GenerateToken(u.ID, u.TenantID, u.Role)
	if err != nil {
		return nil, "", err
	}
	observability.WithContext(observability.WithTenantUser(ctx, u.TenantID, u.ID)).Info("用户登录",
		map[string]interface{}{"operation": "login"})
	return u, token, nil
}

// RegisterMember 在指定租户内注册工作人员（默认 member）。
func RegisterMember(ctx context.Context, tenantID uint64, username, password string) (*model.User, error) {
	t, err := storage.GetTenantByID(tenantID)
	if err != nil || t.Status != 1 {
		return nil, ErrTenantNotFound
	}
	ex, err := storage.IsUsernameExists(tenantID, username)
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
	observability.WithContext(observability.WithTenantUser(ctx, tenantID, u.ID)).Info("注册工作人员",
		map[string]interface{}{"operation": "register_member"})
	return u, nil
}
