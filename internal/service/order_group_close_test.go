package service

import (
	"errors"
	"testing"
)

func TestGroupCloseNeedsConfirmErrorUnwrap(t *testing.T) {
	err := &GroupCloseNeedsConfirmError{
		ProductID:         1,
		PendingTeamCount:  4,
		PendingOrderCount: 4,
	}
	if !errors.Is(err, ErrGroupCloseNeedsConfirm) {
		t.Fatal("expected errors.Is ErrGroupCloseNeedsConfirm")
	}
	msg := err.Error()
	if msg == "" || !errors.Is(err, ErrGroupCloseNeedsConfirm) {
		t.Fatalf("bad error: %s", msg)
	}
}

func TestEnsureCanCloseProductGroupNilCloser(t *testing.T) {
	s := &ProductService{}
	if err := s.ensureCanCloseProductGroup(1, true, false, false); err != nil {
		t.Fatalf("nil closer should skip: %v", err)
	}
}

type stubGroupCloser struct {
	teams, orders int64
	failN         int
	failErr       error
	countCalls    int
	failCalls     int
}

func (s *stubGroupCloser) CountPendingProductChannelGroups(uint64) (int64, int64, error) {
	s.countCalls++
	return s.teams, s.orders, nil
}

func (s *stubGroupCloser) FailPendingProductChannelGroups(uint64) (int, error) {
	s.failCalls++
	return s.failN, s.failErr
}

func TestEnsureCanCloseProductGroupConfirm(t *testing.T) {
	stub := &stubGroupCloser{teams: 2, orders: 3}
	s := &ProductService{GroupCloser: stub}
	err := s.ensureCanCloseProductGroup(9, true, false, false)
	var conf *GroupCloseNeedsConfirmError
	if !errors.As(err, &conf) || conf.PendingOrderCount != 3 || conf.PendingTeamCount != 2 {
		t.Fatalf("got %#v", err)
	}
	if stub.failCalls != 0 {
		t.Fatal("should not fail teams without force")
	}
}

func TestEnsureCanCloseProductGroupForce(t *testing.T) {
	stub := &stubGroupCloser{teams: 2, orders: 3, failN: 2}
	s := &ProductService{GroupCloser: stub}
	if err := s.ensureCanCloseProductGroup(9, true, false, true); err != nil {
		t.Fatal(err)
	}
	if stub.failCalls != 1 {
		t.Fatalf("failCalls=%d", stub.failCalls)
	}
}

func TestEnsureCanCloseProductGroupAlreadyOffForceCleanup(t *testing.T) {
	stub := &stubGroupCloser{teams: 1, orders: 1, failN: 1}
	s := &ProductService{GroupCloser: stub}
	if err := s.ensureCanCloseProductGroup(1, false, false, true); err != nil {
		t.Fatal(err)
	}
	if stub.failCalls != 1 {
		t.Fatalf("expected cleanup failCalls=1, got %d", stub.failCalls)
	}
}

func TestEnsureCanCloseProductGroupAlreadyOffNoForce(t *testing.T) {
	stub := &stubGroupCloser{teams: 1, orders: 1}
	s := &ProductService{GroupCloser: stub}
	if err := s.ensureCanCloseProductGroup(1, false, false, false); err != nil {
		t.Fatal(err)
	}
	if stub.countCalls != 0 {
		t.Fatal("should not query when already off without force")
	}
}
