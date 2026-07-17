package handler

import (
	"errors"

	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/storage"

	"github.com/gin-gonic/gin"
)

type UploadHandler struct {
	Store *storage.LocalStorage
}

// UploadImage godoc
// @Summary      上传图片
// @Description  multipart 字段 file；返回 url 可用于商品 cover_url / images
// @Tags         上传
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        file  formData  file  true  "图片文件"
// @Success      200   {object}  response.Body{data=storage.UploadResult}
// @Failure      400   {object}  response.Body
// @Router       /upload [post]
func (h *UploadHandler) UploadImage(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请上传 file 字段")
		return
	}
	result, err := h.Store.Save(file)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrFileTooLarge):
			response.BadRequest(c, "文件过大")
		case errors.Is(err, storage.ErrInvalidFileType):
			response.BadRequest(c, "仅支持 jpg/png/gif/webp")
		default:
			response.InternalError(c, "上传失败")
		}
		return
	}
	response.OK(c, result)
}
