package wechat

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCode2SessionSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("js_code") != "valid-code" {
			t.Fatalf("unexpected js_code: %s", r.URL.Query().Get("js_code"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openid":"oABC123","session_key":"sk","unionid":"uXYZ"}`))
	}))
	defer srv.Close()

	client := NewClient("appid", "secret")
	client.HTTP = srv.Client()
	client.code2SessionURL = srv.URL

	result, err := client.Code2Session("valid-code")
	if err != nil {
		t.Fatalf("Code2Session() error = %v", err)
	}
	if result.OpenID != "oABC123" || result.UnionID != "uXYZ" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCode2SessionAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":40029,"errmsg":"invalid code"}`))
	}))
	defer srv.Close()

	client := NewClient("appid", "secret")
	client.HTTP = srv.Client()
	client.code2SessionURL = srv.URL

	_, err := client.Code2Session("bad-code")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.ErrCode != 40029 {
		t.Fatalf("expected APIError 40029, got %v", err)
	}
}

func TestUserMessage(t *testing.T) {
	if UserMessage(40029) == "" || UserMessage(99999) == "" {
		t.Fatal("UserMessage should not return empty string")
	}
}

func TestGetPhoneNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":7200}`))
		case r.URL.Path == "/phone":
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","phone_info":{"phoneNumber":"+8613800138000","purePhoneNumber":"13800138000","countryCode":"86"}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewClient("appid", "secret")
	client.HTTP = srv.Client()
	client.accessTokenURL = srv.URL + "/token"
	client.getPhoneURL = srv.URL + "/phone"

	info, err := client.GetPhoneNumber("phone-code")
	if err != nil {
		t.Fatalf("GetPhoneNumber() error = %v", err)
	}
	if info.PurePhoneNumber != "13800138000" {
		t.Fatalf("unexpected phone: %+v", info)
	}
}
