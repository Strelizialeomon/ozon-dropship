package alibaba

// 本文件是 PR #15 重审（两路审核）发现项的回归测试，逐条对应审核评论里的编号。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
)

// ---------- 严重：下单（非幂等）不许盲重试 ----------

// TestCreateOrderNoBlindRetryOn5xx 5xx 时请求可能已在 1688 落地：只能报错交回防重流程，不许重发。
func TestCreateOrderNoBlindRetryOn5xx(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = func(w http.ResponseWriter, r *http.Request, apiName string, _ int) {
			if apiName == "alibaba.trade.fastCreateOrder" {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte("bad gateway"))
				return
			}
			b, _ := os.ReadFile(filepath.Join("testdata", fixtureByAPI[apiName]))
			_, _ = w.Write(b)
		}
	})

	_, err := env.client.CreateOrder(context.Background(), CreateOrderRequest{Address: testAddress(), CargoList: testCargo()})
	if err == nil {
		t.Fatal("5xx 应报错（订单可能已建，不能当作没发生）")
	}
	if got := env.stub.count("alibaba.trade.fastCreateOrder"); got != 1 {
		t.Fatalf("下单在 5xx 后被盲重试成 %d 次请求 —— 可能重复下单", got)
	}
}

// TestCreateOrderNoBlindRetryOnGarbageBody 回包解不出来同样可能是「订单已建、回包烂」：不重试。
func TestCreateOrderNoBlindRetryOnGarbageBody(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = func(w http.ResponseWriter, r *http.Request, apiName string, _ int) {
			if apiName == "alibaba.trade.fastCreateOrder" {
				_, _ = w.Write([]byte("<html>不是 JSON</html>"))
				return
			}
			b, _ := os.ReadFile(filepath.Join("testdata", fixtureByAPI[apiName]))
			_, _ = w.Write(b)
		}
	})

	_, err := env.client.CreateOrder(context.Background(), CreateOrderRequest{Address: testAddress(), CargoList: testCargo()})
	if err == nil {
		t.Fatal("应报错")
	}
	if got := env.stub.count("alibaba.trade.fastCreateOrder"); got != 1 {
		t.Fatalf("解包失败后被重试成 %d 次请求 —— 可能重复下单", got)
	}
}

// TestCreateOrderRetriesOnRateLimitCode 确定被拒（超限、没执行）的失败仍按退避重试。
func TestCreateOrderRetriesOnRateLimitCode(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = jsonFirstAttempt(t, "alibaba.trade.fastCreateOrder", 1,
			`{"errorCode":"429","errorMessage":"调用频率超限"}`)
	})

	res, err := env.client.CreateOrder(context.Background(), CreateOrderRequest{Address: testAddress(), CargoList: testCargo()})
	if err != nil {
		t.Fatalf("被限流（未执行）应可重试: %v", err)
	}
	if res.OrderID.String() != "58218860983545944" {
		t.Fatalf("orderId = %q", res.OrderID)
	}
	if got := env.stub.count("alibaba.trade.fastCreateOrder"); got != 2 {
		t.Fatalf("尝试次数 = %d, want 2", got)
	}
}

// TestCreateOrderFailedOfferListArray failedOfferList 是数组（官方描述文件 offer[]）；
// 数组形态不许把响应解崩、更不许触发重试。
func TestCreateOrderFailedOfferListArray(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = jsonFirstAttempt(t, "alibaba.trade.fastCreateOrder", 99,
			`{"success":true,"result":{"orderId":"58218860983545944","totalSuccessAmount":4400,"postFee":800,`+
				`"failedOfferList":[{"offerId":612345678901,"specId":"b266e0726506185beaf205cbae88530d","errorCode":"500_004","errorMessage":"库存不足"}]}}`)
	})

	res, err := env.client.CreateOrder(context.Background(), CreateOrderRequest{Address: testAddress(), CargoList: testCargo()})
	if err != nil {
		t.Fatalf("数组形态的 failedOfferList 不应把响应解崩: %v", err)
	}
	if len(res.FailedOfferList) != 1 {
		t.Fatalf("failedOfferList = %+v", res.FailedOfferList)
	}
	if res.FailedOfferList[0].ErrorCode != "500_004" || res.FailedOfferList[0].OfferID.String() != "612345678901" {
		t.Fatalf("failedOfferList[0] = %+v", res.FailedOfferList[0])
	}
	if got := env.stub.count("alibaba.trade.fastCreateOrder"); got != 1 {
		t.Fatalf("尝试次数 = %d, want 1", got)
	}
}

// ---------- 中：订单列表形状守卫（防重核对不能把形状漂移读成「没下过单」） ----------

func TestListOrdersShapeGuard(t *testing.T) {
	for _, body := range []string{`{}`, `{"success":true}`, `{"result":null}`} {
		t.Run(body, func(t *testing.T) {
			env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
				stub.handler = jsonFirstAttempt(t, "alibaba.trade.getBuyerOrderList", 99, body)
			})
			_, err := env.client.ListBuyerOrders(context.Background(), ListBuyerOrdersRequest{})
			var ae *APIError
			if !errors.As(err, &ae) || ae.Code != "BAD_RESPONSE" {
				t.Fatalf("应报 BAD_RESPONSE，得到 %v", err)
			}
			if got := env.stub.count("alibaba.trade.getBuyerOrderList"); got != 1 {
				t.Fatalf("被重试了 %d 次", got)
			}
		})
	}

	// 空结果（result: []）是合法成功，不能误伤。
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = jsonFirstAttempt(t, "alibaba.trade.getBuyerOrderList", 99, `{"result":[],"totalRecord":0}`)
	})
	res, err := env.client.ListBuyerOrders(context.Background(), ListBuyerOrdersRequest{})
	if err != nil {
		t.Fatalf("空结果不应报错: %v", err)
	}
	if len(res.Orders) != 0 || res.TotalRecord != 0 {
		t.Fatalf("空结果解出来不对: %+v", res)
	}
}

// ---------- 中：鉴权失败（token 被轮换/作废）清缓存，下次调用自愈 ----------

func TestAuthFailureInvalidatesTokenCache(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = jsonFirstAttempt(t, "alibaba.createOrder.preview", 99,
			`{"success":false,"errorCode":"InvalidSession","errorMessage":"会话已过期"}`)
	})
	ctx := context.Background()

	_, err := env.client.PreviewOrder(ctx, PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
	if !IsCode(err, "InvalidSession") {
		t.Fatalf("应带 InvalidSession 错误码: %v", err)
	}
	if got := env.stub.count("getToken"); got != 1 {
		t.Fatalf("续期次数 = %d, want 1", got)
	}

	// 缓存已被清：下一次调用重新走续期（而不是拿旧 token 干等本地估算的到期）。
	_, _ = env.client.PreviewOrder(ctx, PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
	if got := env.stub.count("getToken"); got != 2 {
		t.Fatalf("鉴权失败后应重读凭据并续期，续期次数 = %d, want 2", got)
	}
}

// ---------- 中：续期失败冷却窗口（并发不放大、快速失败） ----------

func TestRefreshFailureCooldownSingleFlight(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = func(w http.ResponseWriter, r *http.Request, apiName string, _ int) {
			if apiName == "getToken" {
				time.Sleep(20 * time.Millisecond) // 放大并发窗口
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh_token 已失效"}`))
				return
			}
			b, _ := os.ReadFile(filepath.Join("testdata", fixtureByAPI[apiName]))
			_, _ = w.Write(b)
		}
	})

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if !IsReauthRequired(err) {
			t.Fatalf("第 %d 个调用应拿到可识别的续期失败: %v", i, err)
		}
	}
	if got := env.stub.count("getToken"); got != 1 {
		t.Fatalf("并发下续期打了 %d 次网关, want 1（失败进冷却窗口）", got)
	}

	// 冷却结束（假时钟 +31 秒）后允许再试。
	env.clock.Advance(31 * time.Second)
	_, _ = env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
	if got := env.stub.count("getToken"); got != 2 {
		t.Fatalf("冷却结束后应再试一次，续期次数 = %d, want 2", got)
	}
}

// ---------- 中：回写失败不丢新 token（否则旧 refresh_token 已作废 = 全线断） ----------

func TestRefreshSaveFailureKeepsToken(t *testing.T) {
	env := newTestEnv(t, func(creds *fakeCreds, _ *gatewayStub) {
		creds.putErr = errors.New("DB 抖动")
	})
	ctx := context.Background()

	if _, err := env.client.PreviewOrder(ctx, PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()}); err != nil {
		t.Fatalf("回写失败不应拖垮调用（新 token 已在手）: %v", err)
	}
	if got := env.stub.lastRequest("alibaba.createOrder.preview").form.Get(tokenParam); got != "6100newtokennewtokennewtoken0000" {
		t.Fatalf("业务请求应带新 token，得到 %q", got)
	}
	// 第二次调用命中内存缓存，不再打网关。
	if _, err := env.client.PreviewOrder(ctx, PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()}); err != nil {
		t.Fatalf("第二次调用: %v", err)
	}
	if got := env.stub.count("getToken"); got != 1 {
		t.Fatalf("续期次数 = %d, want 1（新 token 应留在内存里）", got)
	}
}

// ---------- 中：成功码口径（code=0/200 不算错误） ----------

func TestStatusFieldsErrCodes(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"success true + code 0", `{"success":true,"code":"0","message":"成功"}`, false},
		{"code 200 无 success", `{"code":"200","message":"ok"}`, false},
		{"errorCode 0 无 success", `{"errorCode":"0"}`, false},
		{"success false + code 500", `{"success":false,"code":"500","message":"不是自己的订单"}`, true},
		{"业务错误码", `{"success":false,"errorCode":"500_004","errorMessage":"库存不足"}`, true},
		{"success true 但带错误码", `{"success":true,"errorCode":"404","errorMessage":"没有物流跟踪信息"}`, true},
		{"success true 带无码文案（未发货形态）", `{"errorMessage":"该订单没有物流跟踪信息。","success":true}`, false},
		{"无 success 无码只有文案", `{"errorMessage":"出错了"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f statusFields
			if err := json.Unmarshal([]byte(tc.body), &f); err != nil {
				t.Fatalf("解测试体失败: %v", err)
			}
			got := f.err("test.api") != nil
			if got != tc.wantErr {
				t.Fatalf("err() = %v, want %v（body: %s）", got, tc.wantErr, tc.body)
			}
		})
	}
}

// ---------- 轻微：HTTP 状态码分类（3xx 不再当成功） ----------

func TestHTTPStatusErrorClass(t *testing.T) {
	a := api{"com.alibaba.trade", "some.api", 1}

	if err := httpStatusError(a, http.StatusOK, "", nil); err != nil {
		t.Fatalf("2xx 不应报错: %v", err)
	}
	err := httpStatusError(a, http.StatusFound, "", nil)
	var ae *APIError
	if !errors.As(err, &ae) || ae.HTTP != http.StatusFound || !isPermanent(err) {
		t.Fatalf("3xx 应为不重试的状态码错误: %v", err)
	}
	err = httpStatusError(a, http.StatusTooManyRequests, "2", nil)
	var rle *ratelimit.RateLimitedError
	if !errors.As(err, &rle) || rle.RetryAfter != 2*time.Second {
		t.Fatalf("429 应带 Retry-After=2s: %v", err)
	}
	if err = httpStatusError(a, http.StatusBadGateway, "", nil); err == nil || isPermanent(err) {
		t.Fatalf("5xx 应为可重试错误: %v", err)
	}
	if err = httpStatusError(a, http.StatusForbidden, "", nil); err == nil || !isPermanent(err) {
		t.Fatalf("403 应为不重试错误: %v", err)
	}
}

// ---------- 轻微：大整数 id 不炸解码 ----------

func TestFlexStringBigIDs(t *testing.T) {
	// 超过 int64 上限的 id 原样保全，不把整个响应解崩。
	var o Order
	body := `{"baseInfo":{"id":9223372036854775808,"idOfStr":"9223372036854775808"},"productItems":[{"productID":18446744073709551615}]}`
	if err := json.Unmarshal([]byte(body), &o); err != nil {
		t.Fatalf("大整数 id 不应解崩: %v", err)
	}
	if got := o.BaseInfo.ID.String(); got != "9223372036854775808" {
		t.Fatalf("baseInfo.id = %q", got)
	}
	if got := o.ProductItems[0].ProductID.String(); got != "18446744073709551615" {
		t.Fatalf("productID = %q", got)
	}

	var tr LogisticsTrace
	if err := json.Unmarshal([]byte(`{"orderId":188983797838441800}`), &tr); err != nil {
		t.Fatalf("轨迹 orderId 不应解崩: %v", err)
	}
	if got := tr.OrderID.String(); got != "188983797838441800" {
		t.Fatalf("trace orderId = %q", got)
	}
}

// ---------- 轻微：实付金额口径 ----------

func TestPaidAmount(t *testing.T) {
	env := newTestEnv(t, nil)
	order, err := env.client.GetOrder(context.Background(), 58218860983545944)
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if got := order.PaidAmount().String(); got != "6.15" {
		t.Fatalf("PaidAmount = %s, want 6.15（实付=各期 phasAmount 之和）", got)
	}
}
