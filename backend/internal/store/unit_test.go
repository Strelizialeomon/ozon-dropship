package store

import (
	"errors"
	"testing"
)

// 档位判定：距到期天数 → 14 / 7 / 1 / 0（够不上任何档）。
func TestStageForDays(t *testing.T) {
	cases := []struct {
		days float64
		want int
	}{
		{30, 0}, {14.5, 0}, {14.0, 14}, {13.9, 14}, {10, 14},
		{7.0, 7}, {6.9, 7}, {2, 7},
		{1.0, 1}, {0.5, 1}, {0, 1}, {-3, 1}, // 已过期也归最后一档（只发一次）
	}
	for _, c := range cases {
		if got := stageForDays(c.days); got != c.want {
			t.Errorf("stageForDays(%v) = %d, want %d", c.days, got, c.want)
		}
	}
}

// 脱敏：只留尾 4 位。
func TestMaskValue(t *testing.T) {
	cases := map[string]string{
		"":                          "",
		"abc":                       "****", // 不足 4 位整串隐去
		"abcd":                      "****",
		"abcde":                     "****bcde",
		"fake-access-token-abcdefg": "****defg",
	}
	for in, want := range cases {
		if got := maskValue(in); got != want {
			t.Errorf("maskValue(%q) = %q, want %q", in, got, want)
		}
	}
}

// 载荷校验：未知种类 / 缺必填字段。
func TestValidatePayload(t *testing.T) {
	if err := validatePayload("nope", map[string]string{}); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("未知种类应返回 ErrUnknownKind，得到 %v", err)
	}
	if err := validatePayload(KindOzonAPIKey, map[string]string{}); err == nil {
		t.Fatal("缺 api_key 应报错")
	}
	var piv *ErrPayloadInvalid
	if err := validatePayload(KindOzonAPIKey, map[string]string{}); !errors.As(err, &piv) {
		t.Fatalf("应为 *ErrPayloadInvalid，得到 %T", err)
	}
	if err := validatePayload(KindAlibabaApp, map[string]string{"app_key": "k"}); err == nil {
		t.Fatal("1688 应用密钥缺 app_secret 应报错")
	}
	if err := validatePayload(KindAlibabaToken, map[string]string{"access_token": "t"}); err != nil {
		t.Fatalf("access_token 有了就应通过: %v", err)
	}
}
