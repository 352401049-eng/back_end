package handler

import (
	"strconv"

	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type RankHandler struct {
	RankSvc *service.RankService
}

func parseRankLimit(c *gin.Context) int {
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
			if limit > 100 {
				limit = 100
			}
		}
	}
	return limit
}

// ListHotGroups godoc
// @Summary      热拼榜
// @Tags         rank
// @Produce      json
// @Param        limit  query  int  false  "条数，默认50"
// @Success      200  {object}  response.Body{data=[]service.RankHotGroupItem}
// @Router       /rank/hot-groups [get]
func (h *RankHandler) ListHotGroups(c *gin.Context) {
	list, err := h.RankSvc.ListHotGroups(parseRankLimit(c))
	if err != nil {
		c.Error(err)
		response.InternalError(c, "获取热拼榜失败")
		return
	}
	response.OK(c, list)
}

// ListHotSales godoc
// @Summary      热销榜
// @Tags         rank
// @Produce      json
// @Param        limit  query  int  false  "条数，默认50"
// @Success      200  {object}  response.Body{data=[]service.RankSalesItem}
// @Router       /rank/hot-sales [get]
func (h *RankHandler) ListHotSales(c *gin.Context) {
	list, err := h.RankSvc.ListHotSales(parseRankLimit(c))
	if err != nil {
		c.Error(err)
		response.InternalError(c, "获取热销榜失败")
		return
	}
	response.OK(c, list)
}

// ListSaveRank godoc
// @Summary      省钱榜
// @Tags         rank
// @Produce      json
// @Param        limit  query  int  false  "条数，默认50"
// @Success      200  {object}  response.Body{data=[]service.RankSaveItem}
// @Router       /rank/save [get]
func (h *RankHandler) ListSaveRank(c *gin.Context) {
	list, err := h.RankSvc.ListSaveRank(parseRankLimit(c))
	if err != nil {
		c.Error(err)
		response.InternalError(c, "获取省钱榜失败")
		return
	}
	response.OK(c, list)
}
