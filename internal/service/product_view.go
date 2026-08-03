package service

import (
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
)

// PurchaseOption 某种购买方式的可售信息。
type PurchaseOption struct {
	Available    bool    `json:"available"`
	Price        float64 `json:"price"`
	CanUseCoupon bool    `json:"can_use_coupon"`
}

// GroupPurchaseOption 拼团购买选项。
type GroupPurchaseOption struct {
	PurchaseOption
	GroupBuyID          *uint64 `json:"group_buy_id,omitempty"`
	TargetCount         *uint32 `json:"target_count,omitempty"`
	AllowRepeatJoin     uint8   `json:"allow_repeat_join"`
	MaxConcurrentTeams  uint32  `json:"max_concurrent_teams"`
}

// ProductSaleOptions 团购 / 拼团购买的展示与下单参考。
type ProductSaleOptions struct {
	Deal  PurchaseOption      `json:"deal"`
	Group GroupPurchaseOption `json:"group"`
}

// ProductStoreView 用户端商品展示（含购买方式与优惠券、拼团说明）。
type ProductStoreView struct {
	model.Product
	CanGroupBuy           bool                      `json:"can_group_buy"`
	CanUseCoupon          bool                      `json:"can_use_coupon"`
	GroupBuyID            *uint64                   `json:"group_buy_id,omitempty"`
	SaleOptions           ProductSaleOptions        `json:"sale_options"`
	ApplicableMerchantIDs []uint64                  `json:"applicable_merchant_ids"`
	ApplicableMerchants   []ApplicableMerchantBrief `json:"applicable_merchants"`
}

func (s *ProductService) ToStoreView(p *model.Product) *ProductStoreView {
	if p == nil {
		return nil
	}
	gbMap := s.loadActiveGroupBuys([]uint64{p.ID})
	var gb *model.GroupBuy
	if g, ok := gbMap[p.ID]; ok {
		gb = &g
	}
	view := buildProductStoreView(*p, gb)
	briefsMap, idsMap := s.loadApplicableMerchantBriefs([]uint64{p.ID})
	applyApplicableMerchantsToStoreView(&view, idsMap[p.ID], briefsMap[p.ID])
	return &view
}

func (s *ProductService) ToStoreViews(products []model.Product) []ProductStoreView {
	ids := make([]uint64, 0, len(products))
	for i := range products {
		ids = append(ids, products[i].ID)
	}
	gbMap := s.loadActiveGroupBuys(ids)
	briefsMap, idsMap := s.loadApplicableMerchantBriefs(ids)
	views := make([]ProductStoreView, 0, len(products))
	for i := range products {
		var gb *model.GroupBuy
		if g, ok := gbMap[products[i].ID]; ok {
			gb = &g
		}
		view := buildProductStoreView(products[i], gb)
		applyApplicableMerchantsToStoreView(&view, idsMap[products[i].ID], briefsMap[products[i].ID])
		views = append(views, view)
	}
	return views
}

func (s *ProductService) loadActiveGroupBuys(productIDs []uint64) map[uint64]model.GroupBuy {
	out := make(map[uint64]model.GroupBuy)
	if len(productIDs) == 0 {
		return out
	}
	var list []model.GroupBuy
	if err := query.NotDeleted(s.DB).
		Where("product_id IN ? AND status = 1", productIDs).
		Find(&list).Error; err != nil {
		return out
	}
	for i := range list {
		out[list[i].ProductID] = list[i]
	}
	return out
}

func buildProductStoreView(p model.Product, gb *model.GroupBuy) ProductStoreView {
	canCoupon := p.EnableCoupon == 1
	groupConfigured := p.EnableGroup == 1 &&
		p.GroupBuyTargetCount != nil && *p.GroupBuyTargetCount >= 2 &&
		p.GroupBuyPrice != nil && *p.GroupBuyPrice > 0 &&
		(p.EnableDeal != 1 || *p.GroupBuyPrice < p.Price)

	var groupBuyID *uint64
	groupPrice := p.Price
	if groupConfigured {
		groupPrice = *p.GroupBuyPrice
	}
	if gb != nil && groupConfigured {
		groupBuyID = &gb.ID
		groupPrice = gb.GroupPrice
	}
	groupAvailable := groupConfigured && gb != nil
	dealAvailable := p.EnableDeal == 1

	deal := PurchaseOption{
		Available:    dealAvailable,
		Price:        p.Price,
		CanUseCoupon: canCoupon,
	}
	group := GroupPurchaseOption{
		PurchaseOption: PurchaseOption{
			Available:    groupAvailable,
			Price:        groupPrice,
			CanUseCoupon: canCoupon,
		},
		GroupBuyID:      groupBuyID,
		TargetCount:     p.GroupBuyTargetCount,
		AllowRepeatJoin: p.GroupBuyAllowRepeat,
		MaxConcurrentTeams: p.GroupBuyMaxConcurrentTeams,
	}

	return ProductStoreView{
		Product:      p,
		CanGroupBuy:  groupAvailable,
		CanUseCoupon: canCoupon,
		GroupBuyID:   groupBuyID,
		SaleOptions: ProductSaleOptions{
			Deal:  deal,
			Group: group,
		},
	}
}
