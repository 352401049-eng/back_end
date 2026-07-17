package wechat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	code2SessionURL    = "https://api.weixin.qq.com/sns/jscode2session"
	accessTokenURL     = "https://api.weixin.qq.com/cgi-bin/token"
	getPhoneNumberURL  = "https://api.weixin.qq.com/wxa/business/getuserphonenumber"
	accessTokenSkew    = 5 * time.Minute
)

// Client 微信小程序服务端 API 客户端。
type Client struct {
	AppID           string
	Secret          string
	HTTP            *http.Client
	code2SessionURL string
	accessTokenURL  string
	getPhoneURL     string

	tokenMu     sync.Mutex
	accessToken string
	tokenExpire time.Time
}

// Code2SessionResult 微信 jscode2session 响应。
type Code2SessionResult struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

// APIError 微信接口返回的业务错误（errcode != 0）。
type APIError struct {
	ErrCode int
	ErrMsg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("wechat api errcode=%d errmsg=%s", e.ErrCode, e.ErrMsg)
}

// PhoneInfo 微信手机号授权结果。
type PhoneInfo struct {
	PhoneNumber     string `json:"phoneNumber"`
	PurePhoneNumber string `json:"purePhoneNumber"`
	CountryCode     string `json:"countryCode"`
}

// NewClient 创建小程序客户端。
func NewClient(appID, secret string) *Client {
	return &Client{
		AppID:           appID,
		Secret:          secret,
		HTTP:            &http.Client{Timeout: 10 * time.Second},
		code2SessionURL: code2SessionURL,
		accessTokenURL:  accessTokenURL,
		getPhoneURL:     getPhoneNumberURL,
	}
}

// Code2Session 用 wx.login 取得的 code 换取 openid / session_key。
func (c *Client) Code2Session(code string) (*Code2SessionResult, error) {
	q := url.Values{
		"appid":      {c.AppID},
		"secret":     {c.Secret},
		"js_code":    {code},
		"grant_type": {"authorization_code"},
	}
	reqURL := c.code2SessionURL + "?" + q.Encode()

	resp, err := c.HTTP.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("请求微信接口失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取微信响应失败: %w", err)
	}

	var result Code2SessionResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析微信响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return nil, &APIError{ErrCode: result.ErrCode, ErrMsg: result.ErrMsg}
	}
	if result.OpenID == "" {
		return nil, fmt.Errorf("微信响应缺少 openid")
	}

	return &result, nil
}

type accessTokenResult struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
}

type getPhoneNumberResult struct {
	ErrCode   int       `json:"errcode"`
	ErrMsg    string    `json:"errmsg"`
	PhoneInfo PhoneInfo `json:"phone_info"`
}

func (c *Client) getAccessToken() (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpire) {
		return c.accessToken, nil
	}

	q := url.Values{
		"grant_type": {"client_credential"},
		"appid":      {c.AppID},
		"secret":     {c.Secret},
	}
	resp, err := c.HTTP.Get(c.accessTokenURL + "?" + q.Encode())
	if err != nil {
		return "", fmt.Errorf("请求 access_token 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("读取 access_token 响应失败: %w", err)
	}

	var result accessTokenResult
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 access_token 响应失败: %w", err)
	}
	if result.ErrCode != 0 {
		return "", &APIError{ErrCode: result.ErrCode, ErrMsg: result.ErrMsg}
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("微信响应缺少 access_token")
	}

	c.accessToken = result.AccessToken
	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 7200
	}
	c.tokenExpire = time.Now().Add(time.Duration(expiresIn)*time.Second - accessTokenSkew)

	return c.accessToken, nil
}

// GetPhoneNumber 用 getPhoneNumber 组件返回的 code 换取手机号。
func (c *Client) GetPhoneNumber(code string) (*PhoneInfo, error) {
	token, err := c.getAccessToken()
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s?access_token=%s", c.getPhoneURL, url.QueryEscape(token))
	resp, err := c.HTTP.Post(reqURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("请求手机号接口失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取手机号响应失败: %w", err)
	}

	var result getPhoneNumberResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析手机号响应失败: %w", err)
	}
	if result.ErrCode != 0 {
		return nil, &APIError{ErrCode: result.ErrCode, ErrMsg: result.ErrMsg}
	}

	phone := result.PhoneInfo.PurePhoneNumber
	if phone == "" {
		phone = result.PhoneInfo.PhoneNumber
	}
	if phone == "" {
		return nil, fmt.Errorf("微信响应缺少手机号")
	}
	result.PhoneInfo.PurePhoneNumber = phone

	return &result.PhoneInfo, nil
}

// UserMessage 将微信 errcode 转为面向用户的提示。
func UserMessage(errCode int) string {
	switch errCode {
	case 40029:
		return "登录凭证无效，请重新打开小程序"
	case 40163:
		return "登录凭证已使用，请重新登录"
	case 40226:
		return "账号存在风险，暂时无法登录"
	case 45011:
		return "登录请求过于频繁，请稍后再试"
	case 40001:
		return "微信凭证无效，请稍后重试"
	case 40013:
		return "小程序配置错误，请联系管理员"
	case 85079:
		return "小程序未开通手机号能力"
	case -1:
		return "微信服务繁忙，请稍后再试"
	default:
		return "微信登录失败，请稍后再试"
	}
}
