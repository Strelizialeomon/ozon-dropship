package alibaba

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
)

// 凭据种类：必须与 S1-A store 包的 KindAlibabaApp / KindAlibabaToken 一致
// （S1 父 issue #5 的跨份契约；本包不 import store，值写死在这里，改契约两处一起改）。
const (
	kindAlibabaApp   = "alibaba_app"   // 企业级：app_key + app_secret
	kindAlibabaToken = "alibaba_token" // 企业级：access_token（+ refresh_token 可选）
)

// CredentialStore 本包用到的凭据读写能力（只碰企业级凭据，storeID 固定空串）。
// 方法签名与 S1-A store.CredentialService 的 Get/Put/SetExpiry 对齐；
// 装配层在 cmd/api 用一个小适配器接上（本包不 import store，守包依赖 ADR）。
type CredentialStore interface {
	// Get 读明文载荷（storeID 空串 = 企业级凭据），返回载荷与到期时间。
	Get(ctx context.Context, storeID, kind string) (payload map[string]string, expiresAt *time.Time, err error)
	// Put 写入载荷（同 kind 覆盖 = 轮换）；expiresAt 为 nil 时保留原有到期时间。
	Put(ctx context.Context, storeID, kind string, payload map[string]string, expiresAt *time.Time) error
	// SetExpiry 回写到期时间 / 最近校验时间（到期时间被推远时复位告警档）。
	SetExpiry(ctx context.Context, storeID, kind string, expiresAt, lastVerifiedAt *time.Time) error
}

// TokenError 凭据读取或 token 续期失败（可识别错误，总纲 §5.8 的到期告警用）。
// Reauth = true 表示 refresh_token 失效/授权被取消，需要人工重新授权——告警时应单独标出。
type TokenError struct {
	// Op 阶段：load（读凭据）/ refresh（续期）/ save（回写）。
	Op string
	// Reauth 是否需人工重新授权。
	Reauth bool
	// Message 人话描述。
	Message string
	// Err 底层错误（网络失败、*APIError 等）。
	Err error
}

func (e *TokenError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("1688 %s: %s: %v", e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("1688 %s: %s", e.Op, e.Message)
}

func (e *TokenError) Unwrap() error { return e.Err }

// IsReauthRequired 判断 err 是否为「需人工重新授权」的凭据错误。
func IsReauthRequired(err error) bool {
	var te *TokenError
	if errors.As(err, &te) {
		return te.Reauth
	}
	return false
}

// reauthCodes 需要重新授权的错误码（小写比对）。
// 【未验】官方没有公开 refresh_token 失效时的错误码枚举，这里按 OAuth 常见口径收了一组，
// 权限批下来实测后回填（子 spec 验收第 2 条）。
var reauthCodes = map[string]bool{
	"invalid_grant":         true,
	"invalid_client":        true,
	"invalid_session":       true,
	"unauthorized":          true,
	"invalid_refresh_token": true,
	"401":                   true,
}

// isReauthCode 判断错误码是否意味着需重新授权。
func isReauthCode(code string) bool {
	return reauthCodes[strings.ToLower(code)]
}

// 续期策略参数。
const (
	// tokenFreshMargin 快到期的阈值：距失效不足该时长就续。
	tokenFreshMargin = 10 * time.Minute
	// defaultAccessTokenTTL access_token 默认有效期（官方 10 小时，expires_in=36000；
	// 响应缺 expires_in 时按它估算）。
	defaultAccessTokenTTL = 10 * time.Hour
	// appCredsTTL 应用密钥的内存缓存时长（轮换后不用重启进程，最多 10 分钟生效）。
	appCredsTTL = 10 * time.Minute
)

// tokenCredential 一次续期的结果。
type tokenCredential struct {
	accessToken  string
	refreshToken string
	// refreshTokenExpiresAt 新 refresh_token 的到期时间（响应里有时才有）。
	refreshTokenExpiresAt *time.Time
	// accessTokenTTL access_token 有效期。
	accessTokenTTL time.Duration
}

// tokenManager 凭据与 token 的内存缓存 + 自动续期。
// 并发安全：整个续期流程在一把锁里（续期很稀有，不值得做更细的并发控制；
// 顺带天然单飞——并发调用只会触发一次续期）。
type tokenManager struct {
	client *Client
	creds  CredentialStore
	now    func() time.Time

	mu          sync.Mutex
	accessToken string
	// accessExpiresAt 本地估算的 access_token 失效时刻。
	accessExpiresAt time.Time

	appMu sync.Mutex
	app   appCredentials
}

type appCredentials struct {
	key      string
	secret   string
	loadedAt time.Time
}

// appCreds 取应用密钥（内存缓存 appCredsTTL）。
func (m *tokenManager) appCreds(ctx context.Context) (string, string, error) {
	m.appMu.Lock()
	defer m.appMu.Unlock()
	if m.app.key != "" && m.now().Sub(m.app.loadedAt) < appCredsTTL {
		return m.app.key, m.app.secret, nil
	}
	payload, _, err := m.creds.Get(ctx, "", kindAlibabaApp)
	if err != nil {
		return "", "", &TokenError{Op: "load", Message: "读 1688 应用密钥失败", Err: err}
	}
	key, secret := payload["app_key"], payload["app_secret"]
	if key == "" || secret == "" {
		return "", "", &TokenError{Op: "load", Message: "1688 应用密钥不完整（缺 app_key 或 app_secret）"}
	}
	m.app = appCredentials{key: key, secret: secret, loadedAt: m.now()}
	return key, secret, nil
}

// token 取可用的 access_token：缓存有效直接用；否则读库、必要时用 refresh_token 续期并回写。
//
// 冷启动（进程内没缓存）时无法从库里判断 access_token 是否还在有效期（库里记的到期时间是
// refresh_token 的），所以冷启动后第一次调用必然续期一次——这是刻意选择：宁多一次续期，
// 不拿未知状态的 token 去撞网关。
func (m *tokenManager) token(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.accessToken != "" && m.now().Add(tokenFreshMargin).Before(m.accessExpiresAt) {
		return m.accessToken, nil
	}

	appKey, appSecret, err := m.appCreds(ctx)
	if err != nil {
		return "", err
	}

	payload, _, err := m.creds.Get(ctx, "", kindAlibabaToken)
	if err != nil {
		return "", &TokenError{Op: "load", Message: "读 1688 token 失败（credentials 里没有 alibaba_token？）", Err: err}
	}
	access, refresh := payload["access_token"], payload["refresh_token"]

	if refresh == "" {
		if access == "" {
			return "", &TokenError{Op: "load", Reauth: true, Message: "1688 token 未配置，需先完成授权并录入 access_token"}
		}
		// 没有 refresh_token：没法自动续，直接用库里这份（是否过期交给网关判）。
		m.accessToken = access
		m.accessExpiresAt = m.now().Add(defaultAccessTokenTTL)
		return access, nil
	}

	cred, err := m.refresh(ctx, appKey, appSecret, refresh)
	if err != nil {
		return "", &TokenError{Op: "refresh", Reauth: isReauthCode(Code(err)), Message: "1688 token 续期失败", Err: err}
	}
	if err := m.save(ctx, cred, refresh); err != nil {
		return "", err
	}

	m.accessToken = cred.accessToken
	m.accessExpiresAt = m.now().Add(cred.accessTokenTTL)
	return cred.accessToken, nil
}

// save 续期成功后回写凭据：
//   - 载荷（access_token + 新的 refresh_token）用 Put、expiresAt 传 nil（保留库里的
//     refresh_token 到期时间——不传值就不能把告警锚点抹掉）；
//   - 新 refresh_token 的到期时间若响应里给了，用 SetExpiry 推进（只有真的推远了才复位
//     告警档，普通续期到期时间不变、不会反复触发 14/7/1 告警）。
func (m *tokenManager) save(ctx context.Context, cred *tokenCredential, oldRefresh string) error {
	refresh := cred.refreshToken
	if refresh == "" {
		refresh = oldRefresh // 响应没给新 refresh_token：沿用旧的
	}
	payload := map[string]string{
		"access_token":  cred.accessToken,
		"refresh_token": refresh,
	}
	if err := m.creds.Put(ctx, "", kindAlibabaToken, payload, nil); err != nil {
		return &TokenError{Op: "save", Message: "回写 1688 token 失败", Err: err}
	}
	now := m.now().UTC()
	if err := m.creds.SetExpiry(ctx, "", kindAlibabaToken, cred.refreshTokenExpiresAt, &now); err != nil {
		return &TokenError{Op: "save", Message: "回写 1688 token 到期时间失败", Err: err}
	}
	return nil
}

// refresh 调 system.oauth2.getToken 用 refresh_token 换新 access_token。
//
// 【未验】该接口的端点、参数与「无需签名」的口径来自教程与社区实现（官方文档页在
// open.1688.com 开发指南，需登录），等「买家自用版」权限批下来实测；若网关实际要求签名，
// 在这里补 form.Set(signatureParam, sign(...)) 即可（一个函数调用的事）。
// 请求量很小（10 小时一次），同样走限流器。
func (m *tokenManager) refresh(ctx context.Context, appKey, appSecret, refreshToken string) (*tokenCredential, error) {
	c := m.client
	var cred tokenCredential
	err := c.registry.Do(ctx, ratelimit.ScopeAlibaba, subjectApp, apiGetToken.name, func(ctx context.Context) error {
		form := url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("client_id", appKey)
		form.Set("client_secret", appSecret)
		form.Set("refresh_token", refreshToken)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiGetToken.url(c.baseURL, appKey), strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("1688 token 续期请求失败: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit))
		if err != nil {
			return err
		}
		if err := httpStatusError(apiGetToken, resp.StatusCode, resp.Header.Get("Retry-After"), body); err != nil {
			return err
		}

		var raw struct {
			statusFields
			AccessToken         string `json:"access_token"`
			RefreshToken        string `json:"refresh_token"`
			ExpiresIn           int64  `json:"expires_in"`
			RefreshTokenTimeout string `json:"refresh_token_timeout"`
			MemberID            string `json:"memberId"`
		}
		if err := unmarshalBody(apiGetToken, body, &raw); err != nil {
			return err
		}
		if ae := raw.err(apiGetToken.name); ae != nil {
			return fail(ae)
		}
		if raw.AccessToken == "" {
			return ratelimit.Permanent(&APIError{API: apiGetToken.name, Code: "NO_TOKEN", Message: "续期响应缺 access_token", Body: truncateBody(body)})
		}

		ttl := time.Duration(raw.ExpiresIn) * time.Second
		if ttl <= 0 {
			ttl = defaultAccessTokenTTL
		}
		cred = tokenCredential{
			accessToken:    raw.AccessToken,
			refreshToken:   raw.RefreshToken,
			accessTokenTTL: ttl,
		}
		if raw.RefreshTokenTimeout != "" {
			var ft FlexTime
			if err := ft.UnmarshalJSON([]byte(strconv.Quote(raw.RefreshTokenTimeout))); err == nil && ft.Parsed() {
				t := ft.Time
				cred.refreshTokenExpiresAt = &t
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &cred, nil
}
