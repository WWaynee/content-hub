package model

// 数据模型统一约定：
// - 主键：ID uint64，gorm:"primaryKey;autoIncrement"
// - 审计字段：CreatedAt/UpdatedAt time.Time（type:datetime(3)），DeletedAt gorm.DeletedAt（软删）
// - 说明：各表单独定义结构；软删除用 gorm.DeletedAt
