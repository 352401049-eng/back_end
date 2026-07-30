package service

import "testing"

func TestAttachRiderContact_NilRider(t *testing.T) {
	view := &DeliveryView{}
	attachRiderContact(nil, view)
	attachRiderContact(nil, nil)
	viewNil := (*DeliveryView)(nil)
	attachRiderContact(nil, viewNil)
}
