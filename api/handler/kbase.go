package handler

import (
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/middleware"
	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 知识库（目录/文件）HTTP handler。

// 目录：列出某 scope 某父目录的子目录 + 文件。

// ListKbaseDir 列出目录下的子目录和文件（scope + parent_id）。
func ListKbaseDir(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	scope := c.Query("scope") // public / private
	if scope == "" {
		scope = storage.ScopePrivate
	}
	parentID, _ := parseID(c.DefaultQuery("parent_id", "0"))
	dirID, _ := parseID(c.DefaultQuery("dir_id", "0"))

	ownerUserID := userID
	if scope == storage.ScopePublic {
		ownerUserID = 0
	}

	dirs, err := storage.ListDirs(c.Request.Context(), tenantID, scope, ownerUserID, dirID)
	if err != nil {
		response.ServerError(c, "查询目录失败")
		return
	}
	files, err := storage.ListFilesByDir(c.Request.Context(), tenantID, scope, ownerUserID, dirID)
	if err != nil {
		response.ServerError(c, "查询文件失败")
		return
	}
	_ = parentID
	response.Success(c, gin.H{"dirs": dirs, "files": files})
}

// GetKbaseTree 返回 scope 下全部目录（扁平，前端组装为目录树）。
func GetKbaseTree(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	scope := c.Query("scope")
	if scope == "" {
		scope = storage.ScopePrivate
	}
	ownerUserID := userID
	if scope == storage.ScopePublic {
		ownerUserID = 0
	}
	dirs, err := storage.ListAllDirs(c.Request.Context(), tenantID, scope, ownerUserID)
	if err != nil {
		response.ServerError(c, "查询目录树失败")
		return
	}
	response.Success(c, gin.H{"dirs": dirs})
}

// CreateDirReq 建目录请求。
type CreateDirReq struct {
	Scope    string `json:"scope" binding:"required"`      // public/private
	ParentID uint64 `json:"parent_id"`
	Name     string `json:"name" binding:"required,min=1,max=128"`
}

// CreateKbaseDir 建目录。
func CreateKbaseDir(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	var req CreateDirReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	// 权限：公有库仅管理员
	if req.Scope == storage.ScopePublic && middleware.GetRole(c) != storage.RoleAdmin {
		response.Forbidden(c, "仅管理员可操作公有知识库")
		return
	}
	ownerUserID := userID
	if req.Scope == storage.ScopePublic {
		ownerUserID = 0
	}
	d := &model.KbaseDir{
		TenantID:    tenantID,
		Scope:       req.Scope,
		OwnerUserID: ownerUserID,
		ParentID:    req.ParentID,
		Name:        req.Name,
	}
	if err := storage.CreateDir(c.Request.Context(), d); err != nil {
		response.ServerError(c, "建目录失败")
		return
	}
	response.Success(c, gin.H{"id": d.ID})
}

// DeleteKbaseDir 删目录。
func DeleteKbaseDir(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	scope := c.Query("scope")
	if scope == "" {
		scope = storage.ScopePrivate
	}
	dirID, err := parseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效目录 ID")
		return
	}
	ownerUserID := userID
	if scope == storage.ScopePublic {
		ownerUserID = 0
		if middleware.GetRole(c) != storage.RoleAdmin {
			response.Forbidden(c, "仅管理员可操作公有知识库")
			return
		}
	}
	if err := storage.SoftDeleteDir(c.Request.Context(), tenantID, scope, ownerUserID, dirID); err != nil {
		response.ServerError(c, "删除目录失败")
		return
	}
	response.SuccessMessage(c, "已删除", nil)
}

// RenameKbaseDir 重命名目录（仅本 scope 归属者/管理员）。
func RenameKbaseDir(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	scope := c.Query("scope")
	if scope == "" {
		scope = storage.ScopePrivate
	}
	dirID, err := parseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效目录 ID")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		response.BadRequest(c, "请提供新目录名")
		return
	}
	ownerUserID := userID
	if scope == storage.ScopePublic {
		ownerUserID = 0
		if middleware.GetRole(c) != storage.RoleAdmin {
			response.Forbidden(c, "仅管理员可操作公有知识库")
			return
		}
	}
	affected, err := storage.RenameDir(c.Request.Context(), tenantID, scope, ownerUserID, dirID, req.Name)
	if err != nil {
		response.ServerError(c, "重命名目录失败")
		return
	}
	if affected == 0 {
		response.BadRequest(c, "目录不存在或无权操作")
		return
	}
	response.SuccessMessage(c, "已重命名", nil)
}

// UploadFile 上传文档（新建或覆盖）。
// 表单：scope, dir_id, file（multipart）；覆盖时另传 target_file_id。
func UploadFile(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	scope := c.PostForm("scope")
	if scope == "" {
		scope = storage.ScopePrivate
	}
	dirID, _ := strconv.ParseUint(c.PostForm("dir_id"), 10, 64)
	targetFileID, _ := strconv.ParseUint(c.PostForm("target_file_id"), 10, 64)

	if scope == storage.ScopePublic && middleware.GetRole(c) != storage.RoleAdmin {
		response.Forbidden(c, "仅管理员可上传公有知识库")
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请上传文件")
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		response.ServerError(c, "读取文件失败")
		return
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil {
		response.ServerError(c, "读取文件失败")
		return
	}

	ownerUserID := userID
	if scope == storage.ScopePublic {
		ownerUserID = 0
	}

	res, err := service.IngestDocument(c.Request.Context(), service.IngestParams{
		TenantID:     tenantID,
		Scope:        scope,
		OwnerUserID:  ownerUserID,
		DirID:        dirID,
		FileName:     fileHeader.Filename,
		Content:      content,
		TargetFileID: targetFileID,
	})
	if err != nil {
		response.ServerError(c, "上传失败："+err.Error())
		return
	}
	response.Success(c, gin.H{"file_id": res.FileID, "version_id": res.VersionID, "version_md5": res.VersionMd5})
}

// DeleteKbaseFile 删除文件。
func DeleteKbaseFile(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	scope := c.Query("scope")
	if scope == "" {
		scope = storage.ScopePrivate
	}
	fileID, err := parseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效文件 ID")
		return
	}
	ownerUserID := userID
	if scope == storage.ScopePublic {
		ownerUserID = 0
		if middleware.GetRole(c) != storage.RoleAdmin {
			response.Forbidden(c, "仅管理员可操作公有知识库")
			return
		}
	}
	if err := storage.SoftDeleteFile(c.Request.Context(), tenantID, scope, ownerUserID, fileID); err != nil {
		response.ServerError(c, "删除文件失败")
		return
	}
	response.SuccessMessage(c, "已删除", nil)
}

// RenameKbaseFile 重命名文件（仅本 scope 归属者/管理员）。
func RenameKbaseFile(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	scope := c.Query("scope")
	if scope == "" {
		scope = storage.ScopePrivate
	}
	fileID, err := parseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效文件 ID")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		response.BadRequest(c, "请提供新文件名")
		return
	}
	ownerUserID := userID
	if scope == storage.ScopePublic {
		ownerUserID = 0
		if middleware.GetRole(c) != storage.RoleAdmin {
			response.Forbidden(c, "仅管理员可操作公有知识库")
			return
		}
	}
	affected, err := storage.RenameFile(c.Request.Context(), tenantID, scope, ownerUserID, fileID, req.Name)
	if err != nil {
		response.ServerError(c, "重命名文件失败")
		return
	}
	if affected == 0 {
		response.BadRequest(c, "文件不存在或无权操作")
		return
	}
	response.SuccessMessage(c, "已重命名", nil)
}

// PreviewFile 预览（返回预签名 inline URL）。
func PreviewFile(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	fileID, err := parseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效文件 ID")
		return
	}
	f, err := storage.GetFileByID(c.Request.Context(), tenantID, fileID)
	if err != nil {
		response.BadRequest(c, "文件不存在")
		return
	}
	if f.Scope == storage.ScopePrivate && f.OwnerUserID != userID {
		response.Forbidden(c, "无权访问他人私有文件")
		return
	}
	latest, err := storage.GetLatestVersion(c.Request.Context(), fileID)
	if err != nil {
		response.ServerError(c, "查询版本失败")
		return
	}
	url, err := storage.PresignPreviewURL(latest.OSSObjectKey, time.Hour)
	if err != nil {
		response.ServerError(c, "生成预览链接失败")
		return
	}
	_ = f
	response.Success(c, gin.H{"url": url})
}

// GetFileContent 读取文本文件内容（供内置预览，不走浏览器下载）。
func GetFileContent(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	fileID, err := parseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效文件 ID")
		return
	}
	f, err := storage.GetFileByID(c.Request.Context(), tenantID, fileID)
	if err != nil {
		response.BadRequest(c, "文件不存在")
		return
	}
	if f.Scope == storage.ScopePrivate && f.OwnerUserID != userID {
		response.Forbidden(c, "无权访问他人私有文件")
		return
	}
	if !strings.HasPrefix(f.FileType, "text/") && !isTextExt(f.Name) {
		response.BadRequest(c, "暂不支持该类型文件预览")
		return
	}
	latest, err := storage.GetLatestVersion(c.Request.Context(), fileID)
	if err != nil {
		response.ServerError(c, "查询版本失败")
		return
	}
	data, err := storage.DownloadFile(latest.OSSObjectKey)
	if err != nil {
		response.ServerError(c, "读取文件内容失败")
		return
	}
	response.Success(c, gin.H{"name": f.Name, "content": string(data)})
}

// DownloadFile 下载（返回预签名 attachment URL）。
func DownloadFile(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	fileID, err := parseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效文件 ID")
		return
	}
	f, err := storage.GetFileByID(c.Request.Context(), tenantID, fileID)
	if err != nil {
		response.BadRequest(c, "文件不存在")
		return
	}
	if f.Scope == storage.ScopePrivate && f.OwnerUserID != userID {
		response.Forbidden(c, "无权访问他人私有文件")
		return
	}
	latest, err := storage.GetLatestVersion(c.Request.Context(), fileID)
	if err != nil {
		response.ServerError(c, "查询版本失败")
		return
	}
	url, err := storage.PresignDownloadURL(latest.OSSObjectKey, f.Name, time.Hour)
	if err != nil {
		response.ServerError(c, "生成下载链接失败")
		return
	}
	response.Success(c, gin.H{"url": url})
}

// isTextExt 判断文件名是否为可预览的纯文本扩展名。
func isTextExt(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".md", ".markdown", ".json", ".csv", ".log", ".text":
		return true
	}
	return false
}
