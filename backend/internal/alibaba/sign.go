package alibaba

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

// 1688 网关的协议字段名。
const (
	// signatureParam 签名参数（不参与签名计算本身）。
	signatureParam = "_aop_signature"
	// timestampParam 请求时间戳（毫秒）。官方文档标可选；现代可用实现都会带，
	// 带上更利于网关侧排障，且它正常参与签名。
	timestampParam = "_aop_timestamp"
	// tokenParam 买家授权的 access_token 字段名。
	tokenParam = "access_token"
)

// signPath 拼签名路径：param2/{version}/{namespace}/{name}/{appKey}。
// 依据：官方 Java SDK 的 buildInvokeUrlPath（protocol/version/namespace/name/appKey），
// protocol 固定为 param2；测试参考向量即用此形状生成。
func signPath(version int, namespace, name, appKey string) string {
	return "param2/" + strconv.Itoa(version) + "/" + namespace + "/" + name + "/" + appKey
}

// sign 计算 _aop_signature，返回大写十六进制串。
//
// 规则（官方签名口径，三个独立实现一致）：
//  1. 除 _aop_signature 自身外，所有实际发送的参数都参与（含 access_token、_aop_timestamp）；
//  2. 参数按名称 ASCII 升序排序。
//     注：官方 Java SDK 的实现里排的是「参数名+参数值」拼接串——两种排法仅在
//     「某个参数名是另一个参数名的前缀」时结果不同，本包发送的参数集不会出现这种键，
//     故完全等价；这里按官方文档原文（按参数名排序）实现。
//  3. 直接拼接 key+value 原文（不做 URL 编码、不加分隔符），前面接签名路径；
//  4. 用 appSecret 做 HMAC-SHA1，输出大写十六进制（小写会被网关拒绝）。
func sign(path, appSecret string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == signatureParam {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	mac := hmac.New(sha1.New, []byte(appSecret))
	mac.Write([]byte(path))
	for _, k := range keys {
		mac.Write([]byte(k))
		mac.Write([]byte(params[k]))
	}
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}
