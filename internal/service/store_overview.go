package service

import "yujixinjiang/backend/internal/model"

// MerchantStoreOverview 用户端店铺聚合数据。
type MerchantStoreOverview struct {
	Merchant         model.MerchantProfile `json:"merchant"`
	DeliveryZone     *DeliveryZoneView     `json:"delivery_zone,omitempty"`
	ActiveActivities []ActivityStoreView   `json:"active_activities"`
	ClaimableCoupons []ClaimableCouponView `json:"claimable_coupons"`
	GroupBuyProducts []ProductStoreView    `json:"group_buy_products"`
}
