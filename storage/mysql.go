package storage

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/WWaynee/content-hub/config"
)

// MySQL 连接管理（内存持有全局 DB）
var mysqlDB *gorm.DB

// InitMySQL 初始化 GORM + MySQL 连接，并 Ping 验证连通。
func InitMySQL(cfg *config.MySQL) (*gorm.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local&timeout=10s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			SingularTable: false, // 默认复数表名（与 db.md 一致）
		},
	})
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层 sql.DB 失败: %w", err)
	}
	// 连接池
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(20)

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("Ping MySQL 失败: %w", err)
	}

	mysqlDB = db
	return db, nil
}

// GetDB 返回全局 MySQL DB（InitMySQL 之后调用）。
func GetDB() *gorm.DB { return mysqlDB }
