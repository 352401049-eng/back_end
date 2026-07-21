package service

import (
	"math"
	"sort"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

const rankListLimit = 50

// RankHotGroupItem 热拼榜：进行中的团。
type RankHotGroupItem struct {
	TeamID        uint64    `json:"team_id"`
	GroupBuyID    uint64    `json:"group_buy_id"`
	ProductID     uint64    `json:"product_id"`
	MerchantID    uint64    `json:"merchant_id"`
	ProductName   string    `json:"product_name"`
	ProductCover  string    `json:"product_cover"`
	GroupPrice    float64   `json:"group_price"`
	OriginalPrice float64   `json:"original_price"`
	TargetCount   uint32    `json:"target_count"`
	CurrentCount  uint32    `json:"current_count"`
	NeedCount     uint32    `json:"need_count"`
	Progress      float64   `json:"progress"`
	ExpireAt      time.Time `json:"expire_at"`
	MemberNames   []string  `json:"member_names"`
}

// RankSalesItem 热销榜：按销量。
type RankSalesItem struct {
	ProductID     uint64  `json:"product_id"`
	MerchantID    uint64  `json:"merchant_id"`
	ProductName   string  `json:"product_name"`
	ProductCover  string  `json:"product_cover"`
	Price         float64 `json:"price"`
	OriginalPrice float64 `json:"original_price"`
	SalesCount    uint32  `json:"sales_count"`
	Rank          int     `json:"rank"`
}

// RankSaveItem 省钱榜：按省多少钱。
type RankSaveItem struct {
	ProductID         uint64  `json:"product_id"`
	MerchantID        uint64  `json:"merchant_id"`
	ProductName       string  `json:"product_name"`
	ProductCover      string  `json:"product_cover"`
	CurrentPrice      float64 `json:"current_price"`
	OriginalPrice     float64 `json:"original_price"`
	SaveAmount        float64 `json:"save_amount"`
	SavePercent       int     `json:"save_percent"`
	Tag               string  `json:"tag,omitempty"`
	ActivityID        *uint64 `json:"activity_id,omitempty"`
	ActivityProductID *uint64 `json:"activity_product_id,omitempty"`
	Rank              int     `json:"rank"`
}

type RankService struct {
	DB *gorm.DB
}

func (s *RankService) freshDB() *gorm.DB {
	return s.DB.Session(&gorm.Session{NewDB: true})
}

// ListHotGroups 进行中的拼团，按「还差人数」升序、已拼人数降序。
func (s *RankService) ListHotGroups(limit int) ([]RankHotGroupItem, error) {
	if limit < 1 {
		limit = rankListLimit
	}
	now := time.Now()

	type row struct {
		TeamID        uint64
		GroupBuyID    uint64
		ProductID     uint64
		MerchantID    uint64
		ProductName   string
		ProductCover  string
		GroupPrice    float64
		ProductPrice  float64
		OriginalPrice *float64
		TargetCount   uint32
		CurrentCount  uint32
		ExpireAt      time.Time
	}

	var rows []row
	// 不要求 gb.status=1：团已开出后即使商品拼团配置被关掉，未成团仍应出现在热拼榜
	err := s.freshDB().Table("group_buy_team AS t").
		Select(`t.id AS team_id, t.group_buy_id, gb.product_id, p.merchant_id,
			p.name AS product_name, p.cover_url AS product_cover,
			COALESCE(NULLIF(p.group_buy_price, 0), gb.group_price) AS group_price,
			p.price AS product_price, p.original_price,
			t.target_count, t.current_count, t.expire_at`).
		Joins("JOIN group_buy AS gb ON gb.id = t.group_buy_id AND gb.is_deleted = 0").
		Joins("JOIN product AS p ON p.id = gb.product_id AND p.is_deleted = 0 AND p.status = ?", model.ProductStatusOn).
		Where("t.is_deleted = 0 AND t.status = ? AND t.expire_at > ?", model.GroupBuyTeamPending, now).
		Where("t.current_count < t.target_count").
		Order("t.target_count - t.current_count ASC, t.current_count DESC, t.id DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	teamIDs := make([]uint64, 0, len(rows))
	for _, r := range rows {
		teamIDs = append(teamIDs, r.TeamID)
	}
	namesByTeam := s.loadTeamMemberNames(teamIDs)

	out := make([]RankHotGroupItem, 0, len(rows))
	for _, r := range rows {
		need := uint32(0)
		if r.TargetCount > r.CurrentCount {
			need = r.TargetCount - r.CurrentCount
		}
		progress := 0.0
		if r.TargetCount > 0 {
			progress = float64(r.CurrentCount) / float64(r.TargetCount)
		}
		origin := r.ProductPrice
		if r.OriginalPrice != nil && *r.OriginalPrice > origin {
			origin = *r.OriginalPrice
		}
		out = append(out, RankHotGroupItem{
			TeamID: r.TeamID, GroupBuyID: r.GroupBuyID,
			ProductID: r.ProductID, MerchantID: r.MerchantID,
			ProductName: r.ProductName, ProductCover: r.ProductCover,
			GroupPrice: r.GroupPrice, OriginalPrice: origin,
			TargetCount: r.TargetCount, CurrentCount: r.CurrentCount,
			NeedCount: need, Progress: progress, ExpireAt: r.ExpireAt,
			MemberNames: namesByTeam[r.TeamID],
		})
	}
	return out, nil
}

func (s *RankService) loadTeamMemberNames(teamIDs []uint64) map[uint64][]string {
	result := make(map[uint64][]string)
	if len(teamIDs) == 0 {
		return result
	}
	type mrow struct {
		TeamID   uint64
		Nickname string
	}
	var rows []mrow
	_ = s.freshDB().Table("group_buy_member AS m").
		Select("m.team_id, COALESCE(NULLIF(a.nickname,''), '拼友') AS nickname").
		Joins("JOIN account AS a ON a.id = m.account_id").
		Where("m.is_deleted = 0 AND m.team_id IN ?", teamIDs).
		Order("m.is_leader DESC, m.id ASC").
		Scan(&rows).Error
	for _, r := range rows {
		list := result[r.TeamID]
		if len(list) >= 3 {
			continue
		}
		name := r.Nickname
		if name == "" {
			name = "拼友"
		}
		// 取首字展示
		runes := []rune(name)
		if len(runes) > 1 {
			name = string(runes[0])
		}
		result[r.TeamID] = append(list, name)
	}
	return result
}

// ListHotSales 全站热销，按已支付有效订单销量。
func (s *RankService) ListHotSales(limit int) ([]RankSalesItem, error) {
	if limit < 1 {
		limit = rankListLimit
	}
	type row struct {
		ProductID     uint64
		ProductName   string
		MerchantID    uint64
		CoverURL      string
		Price         float64
		OriginalPrice *float64
		SalesCount    uint32
	}
	var rows []row
	err := s.freshDB().Model(&model.OrderItem{}).
		Select(`order_item.product_id, product.name AS product_name, product.merchant_id,
			product.cover_url, product.price, product.original_price,
			SUM(order_item.quantity) AS sales_count`).
		Joins("JOIN `order` ON `order`.id = order_item.order_id AND `order`.is_deleted = 0").
		Joins("JOIN product ON product.id = order_item.product_id AND product.is_deleted = 0 AND product.status = ?", model.ProductStatusOn).
		Where("order_item.is_deleted = 0").
		Where("`order`.pay_status = ?", model.PayStatusPaid).
		Where("`order`.status NOT IN ?", invalidOrderStatusInts).
		Where("`order`.merchant_review_stage != ?", model.MerchantReviewRejected).
		Group("order_item.product_id, product.name, product.merchant_id, product.cover_url, product.price, product.original_price").
		Order("sales_count DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]RankSalesItem, 0, len(rows))
	for i, r := range rows {
		origin := r.Price
		if r.OriginalPrice != nil && *r.OriginalPrice > origin {
			origin = *r.OriginalPrice
		}
		out = append(out, RankSalesItem{
			ProductID: r.ProductID, MerchantID: r.MerchantID,
			ProductName: r.ProductName, ProductCover: r.CoverURL,
			Price: r.Price, OriginalPrice: origin,
			SalesCount: r.SalesCount, Rank: i + 1,
		})
	}
	return out, nil
}

type saveCandidate struct {
	item RankSaveItem
}

// ListSaveRank 省钱榜：现售价（含活动/拼团）相对原价省得越多越靠前。
func (s *RankService) ListSaveRank(limit int) ([]RankSaveItem, error) {
	if limit < 1 {
		limit = rankListLimit
	}
	now := time.Now()

	var products []model.Product
	if err := query.NotDeleted(s.freshDB()).
		Where("status = ?", model.ProductStatusOn).
		Find(&products).Error; err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return []RankSaveItem{}, nil
	}

	prodByID := make(map[uint64]*model.Product, len(products))
	for i := range products {
		prodByID[products[i].ID] = &products[i]
	}

	// 进行中活动商品的最优现价
	type actPrice struct {
		ActivityID        uint64
		ActivityProductID uint64
		Price             float64
		Tag               string
	}
	bestAct := make(map[uint64]actPrice)

	var activities []model.Activity
	if err := query.NotDeleted(s.freshDB()).
		Where("status = ? AND start_at <= ? AND end_at >= ?", model.ActivityStatusOn, now, now).
		Find(&activities).Error; err != nil {
		return nil, err
	}
	if len(activities) > 0 {
		actIDs := make([]uint64, 0, len(activities))
		for _, a := range activities {
			actIDs = append(actIDs, a.ID)
		}
		var aps []model.ActivityProduct
		if err := query.NotDeleted(s.freshDB()).
			Where("activity_id IN ? AND status = 1", actIDs).
			Find(&aps).Error; err != nil {
			return nil, err
		}
		for _, ap := range aps {
			price := ap.ActivityPrice
			tag := "活动价"
			if ap.EnableGroupBuy == 1 && ap.GroupBuyPrice != nil && *ap.GroupBuyPrice > 0 && *ap.GroupBuyPrice < price {
				price = *ap.GroupBuyPrice
				tag = "拼团价"
			}
			// 秒杀感：活动价低于商品价时标秒杀中（与首页秒杀同源）
			if p, ok := prodByID[ap.ProductID]; ok && ap.ActivityPrice < p.Price {
				if tag != "拼团价" {
					tag = "秒杀中"
				}
			}
			cur, ok := bestAct[ap.ProductID]
			if !ok || price < cur.Price {
				bestAct[ap.ProductID] = actPrice{
					ActivityID: ap.ActivityID, ActivityProductID: ap.ID,
					Price: price, Tag: tag,
				}
			}
		}
	}

	cands := make([]saveCandidate, 0, len(products))
	for i := range products {
		p := &products[i]
		origin := p.Price
		if p.OriginalPrice != nil && *p.OriginalPrice > origin {
			origin = *p.OriginalPrice
		}

		current := p.Price
		tag := ""
		var actID, apID *uint64

		if ap, ok := bestAct[p.ID]; ok && ap.Price > 0 && ap.Price < current {
			current = ap.Price
			tag = ap.Tag
			aid, pid := ap.ActivityID, ap.ActivityProductID
			actID, apID = &aid, &pid
		}
		if p.EnableGroupBuy == 1 && p.GroupBuyPrice != nil && *p.GroupBuyPrice > 0 && *p.GroupBuyPrice < current {
			current = *p.GroupBuyPrice
			tag = "拼团价"
			actID, apID = nil, nil
		}

		save := math.Round((origin-current)*100) / 100
		if save <= 0 {
			continue
		}
		pct := 0
		if origin > 0 {
			pct = int(math.Round(save / origin * 100))
		}
		cands = append(cands, saveCandidate{item: RankSaveItem{
			ProductID: p.ID, MerchantID: p.MerchantID,
			ProductName: p.Name, ProductCover: p.CoverURL,
			CurrentPrice: current, OriginalPrice: origin,
			SaveAmount: save, SavePercent: pct, Tag: tag,
			ActivityID: actID, ActivityProductID: apID,
		}})
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].item.SaveAmount != cands[j].item.SaveAmount {
			return cands[i].item.SaveAmount > cands[j].item.SaveAmount
		}
		return cands[i].item.SavePercent > cands[j].item.SavePercent
	})
	if len(cands) > limit {
		cands = cands[:limit]
	}

	out := make([]RankSaveItem, 0, len(cands))
	for i, c := range cands {
		item := c.item
		item.Rank = i + 1
		out = append(out, item)
	}
	return out, nil
}
