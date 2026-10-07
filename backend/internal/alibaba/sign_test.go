package alibaba

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"testing"
)

// hmacSha1Hex 测试里的独立实现（不经过 sign），用于手工核对拼接顺序。
func hmacSha1Hex(data, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(data))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

// TestSignOfficialVectors 验收第 1 条：同一组参数，签名结果与官方 Java SDK 一致。
//
// 参考值由 testdata/signvec 里的驱动（直接调用官方 SDK 源码
// com.alibaba.tuna.util.SignatureUtil）真跑生成；本表的 path/params/want 与
// SignVec.java 里的向量逐项一致，重新生成步骤见 testdata/signvec/README.md。
func TestSignOfficialVectors(t *testing.T) {
	const secret = "test_app_secret_0123456789abcdef"
	cases := []struct {
		name   string
		path   string
		params map[string]string
		want   string
	}{
		{
			name: "V1 fastCreateOrder 全参数（中文、嵌套 JSON）",
			path: "param2/1/com.alibaba.trade/alibaba.trade.fastCreateOrder/1234567",
			params: map[string]string{
				"access_token":   "6100abcdef6100abcdef6100abcdef",
				"_aop_timestamp": "1728273600000",
				"flow":           "general",
				"message":        "OZON-0001-0001",
				"addressParam":   `{"fullName":"张三","mobile":"13800138000","phone":"0571-88888888","postCode":"310000","provinceText":"浙江省","cityText":"杭州市","areaText":"西湖区","townText":"","address":"文一西路 969 号","districtCode":""}`,
				"cargoParamList": `[{"offerId":612345678901,"specId":"b266e0726506185beaf205cbae88530d","quantity":2}]`,
			},
			want: "B3A46F477583CA62A417EF812833BEFD91C5A9AF",
		},
		{
			name: "V2 预览接口",
			path: "param2/1/com.alibaba.trade/alibaba.createOrder.preview/1234567",
			params: map[string]string{
				"access_token":   "6100abcdef6100abcdef6100abcdef",
				"_aop_timestamp": "1728273600000",
				"flow":           "general",
				"addressParam":   `{"fullName":"李四","mobile":"13900139000","provinceText":"广东省","cityText":"深圳市","areaText":"南山区","address":"科技园路 1 号"}`,
				"cargoParamList": `[{"offerId":612345678902,"specId":"2ba3d63866a71fbae83909d9b4814f01","quantity":5}]`,
			},
			want: "92A5ABA99CC271C1DBF81FDB2EAD3052933C83D7",
		},
		{
			name: "V3 订单列表",
			path: "param2/1/com.alibaba.trade/alibaba.trade.getBuyerOrderList/1234567",
			params: map[string]string{
				"access_token":    "tok-simple-0001",
				"createStartTime": "2026-10-01 00:00:00",
				"createEndTime":   "2026-10-01 23:59:59",
				"isHis":           "false",
				"orderStatus":     "waitbuyerreceive",
				"page":            "1",
				"pageSize":        "50",
				"webSite":         "1688",
			},
			want: "EC0BC1B49C629EDFD286D936EFC6E497D673EADA",
		},
		{
			name: "V4 空值 + _aop_signature 不参与签名",
			path: "param2/1/com.alibaba.logistics/alibaba.trade.getLogisticsInfos.buyerView/1234567",
			params: map[string]string{
				"access_token":   "tok-simple-0002",
				"emptyVal":       "",
				"orderId":        "58218860983545944",
				"fields":         "",
				"webSite":        "1688",
				"_aop_signature": "SHOULD_BE_IGNORED",
			},
			want: "CE4D74D68081AECD6B36769CE5F4AB2FF643E7CE",
		},
		{
			name: "V5 特殊字符（&、=、空格、引号、反斜杠）",
			path: "param2/1/com.alibaba.trade/alibaba.trade.fastCreateOrder/1234567",
			params: map[string]string{
				"access_token": "tok",
				"message":      `备注 & 带=号 还有空格 "引号"`,
				"nested":       `{"k":"v&v"}`,
				"path":         `a/b\c`,
			},
			want: "FC31C04F37FAE30F7B57482CB3340AB460E84378",
		},
		{
			name: "V6 订单详情",
			path: "param2/1/com.alibaba.trade/alibaba.trade.get.buyerView/1234567",
			params: map[string]string{
				"access_token":   "tok-simple-0003",
				"includeFields":  "NativeLogistics",
				"orderId":        "58218860983545944",
				"webSite":        "1688",
				"_aop_timestamp": "1728273600001",
			},
			want: "165519CCFC20E74B135CC0133C993AD279F1FD7E",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sign(tc.path, secret, tc.params)
			if got != tc.want {
				t.Fatalf("签名与官方 SDK 不一致\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// TestSignProperties 签名的几个结构性质。
func TestSignProperties(t *testing.T) {
	const (
		path   = "param2/1/com.alibaba.trade/alibaba.trade.fastCreateOrder/1234567"
		secret = "s3cret"
	)

	// 输出恒为大写十六进制、长度 40（SHA1）。
	sig := sign(path, secret, map[string]string{"a": "1"})
	if len(sig) != 40 || strings.ToUpper(sig) != sig {
		t.Fatalf("签名应为 40 位大写十六进制，得到 %q", sig)
	}

	// _aop_signature 自身不参与：带不带它结果一致。
	with := sign(path, secret, map[string]string{"a": "1", signatureParam: "WHATEVER"})
	if with != sig {
		t.Fatalf("_aop_signature 应被排除：带 %q 与不带 %q 不一致", with, sig)
	}

	// 参数顺序无关（内部先排序）。
	a := sign(path, secret, map[string]string{"b": "2", "a": "1"})
	b := sign(path, secret, map[string]string{"a": "1", "b": "2"})
	if a != b {
		t.Fatal("参数顺序不应影响签名")
	}

	// 按「参数名」排序（官方文档口径）。官方 Java SDK 实现里排的是 key+value 拼接串，
	// 两种排法仅在「某个参数名是另一个参数名的前缀」时不同——本包发送的参数集不含这种键，
	// 这里用一组前缀键钉住我们的选择：按名排序时 "ab" 在 "b" 之前，拼接串排序会得出另一结果。
	prefixed := sign(path, secret, map[string]string{"a": "z", "ab": "0", "b": "1"})
	manual := hmacSha1Hex(path+"a"+"z"+"ab"+"0"+"b"+"1", secret)
	if prefixed != manual {
		t.Fatal("应按参数名 ASCII 升序拼接")
	}
}

// TestSignPrefixKeyPair page/pageSize 是真实存在的「前缀键对」：本项目按官方文档口径
// 按参数名排序，官方 Java SDK 排的是拼接串——对数字页码两种排法必须一致（重审发现项）。
func TestSignPrefixKeyPair(t *testing.T) {
	const (
		path   = "param2/1/com.alibaba.trade/alibaba.trade.getBuyerOrderList/1234567"
		secret = "s3cret"
	)
	for _, page := range []string{"1", "9", "50", "999999"} {
		// 前提：按拼接串排序时 "page"+页码 必须仍排在 "pageSize"+值 前面（数字首字符 < 'S'）。
		if "page"+page >= "pageSize50" {
			t.Fatalf("page=%s：拼接串排序会分叉，按名排序的前提不成立", page)
		}
		params := map[string]string{"page": page, "pageSize": "50"}
		want := hmacSha1Hex(path+"page"+page+"pageSize50", secret)
		if got := sign(path, secret, params); got != want {
			t.Fatalf("page=%s：按名排序结果与预期不符\n got: %s\nwant: %s", page, got, want)
		}
	}
}

// TestSignPath 签名路径拼装。
func TestSignPath(t *testing.T) {
	got := signPath(1, "com.alibaba.trade", "alibaba.trade.fastCreateOrder", "1234567")
	want := "param2/1/com.alibaba.trade/alibaba.trade.fastCreateOrder/1234567"
	if got != want {
		t.Fatalf("signPath = %q, want %q", got, want)
	}
}
