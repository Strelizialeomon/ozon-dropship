package alibaba

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
)

// testAppSecret 测试用 app_secret（需与 testdata/signvec 里签名向量的密钥区分开——
// 向量用 test_app_secret_0123456789abcdef 生成，这里保持同样的串以便手工核对）。
const testAppSecret = "test_app_secret_0123456789abcdef"

// ---------- 测试替身：内存凭据 + 假网关 ----------

// fakeCreds 内存版 CredentialStore，记录读写次数供断言。
type fakeCreds struct {
	mu       sync.Mutex
	payloads map[string]map[string]string // kind → 载荷
	expires  map[string]*time.Time        // kind → 到期时间
	gets     map[string]int               // kind → Get 次数
	puts     []putCall
	setExps  []setExpiryCall
	putErr   error // 非 nil = Put 一律失败（模拟写库故障）
}

type putCall struct {
	kind      string
	payload   map[string]string
	expiresAt *time.Time
}

type setExpiryCall struct {
	kind           string
	expiresAt      *time.Time
	lastVerifiedAt *time.Time
}

func newFakeCreds() *fakeCreds {
	return &fakeCreds{
		payloads: map[string]map[string]string{},
		expires:  map[string]*time.Time{},
		gets:     map[string]int{},
	}
}

func (f *fakeCreds) Get(_ context.Context, storeID, kind string) (map[string]string, *time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if storeID != "" {
		return nil, nil, errors.New("测试替身只支持企业级凭据")
	}
	f.gets[kind]++
	p, ok := f.payloads[kind]
	if !ok {
		return nil, nil, errors.New("凭据不存在")
	}
	cp := make(map[string]string, len(p))
	for k, v := range p {
		cp[k] = v
	}
	var exp *time.Time
	if e := f.expires[kind]; e != nil {
		t := *e
		exp = &t
	}
	return cp, exp, nil
}

func (f *fakeCreds) Put(_ context.Context, _ string, kind string, payload map[string]string, expiresAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return f.putErr
	}
	cp := make(map[string]string, len(payload))
	for k, v := range payload {
		cp[k] = v
	}
	f.puts = append(f.puts, putCall{kind: kind, payload: cp, expiresAt: expiresAt})
	f.payloads[kind] = cp
	if expiresAt != nil {
		f.expires[kind] = expiresAt
	}
	return nil
}

func (f *fakeCreds) SetExpiry(_ context.Context, _ string, kind string, expiresAt, lastVerifiedAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setExps = append(f.setExps, setExpiryCall{kind: kind, expiresAt: expiresAt, lastVerifiedAt: lastVerifiedAt})
	if expiresAt != nil {
		f.expires[kind] = expiresAt
	}
	return nil
}

// gatewayStub 假 1688 网关：记录每次请求的路径与表单，默认按接口名回对应 fixture。
type gatewayStub struct {
	t        *testing.T
	mu       sync.Mutex
	attempts map[string]int
	reqs     []stubRequest
	handler  func(w http.ResponseWriter, r *http.Request, apiName string, attempt int)
}

type stubRequest struct {
	path string
	form url.Values
}

func newGatewayStub(t *testing.T) *gatewayStub {
	return &gatewayStub{t: t, attempts: map[string]int{}}
}

// apiNameFromPath 从 .../param2/1/{namespace}/{name}/{appKey} 里取接口名。
func apiNameFromPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 5 {
		return ""
	}
	return parts[4]
}

func (s *gatewayStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := apiNameFromPath(r.URL.Path)

	s.mu.Lock()
	s.attempts[name]++
	attempt := s.attempts[name]
	s.reqs = append(s.reqs, stubRequest{path: r.URL.Path, form: r.PostForm})
	h := s.handler
	s.mu.Unlock()

	if h != nil {
		h(w, r, name, attempt)
		return
	}
	file, ok := fixtureByAPI[name]
	if !ok {
		s.t.Errorf("网关替身没有为接口 %q 配响应（path=%s）", name, r.URL.Path)
		http.NotFound(w, r)
		return
	}
	b, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		s.t.Fatalf("读 fixture %s 失败: %v", file, err)
	}
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	_, _ = w.Write(b)
}

func (s *gatewayStub) count(apiName string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts[apiName]
}

// lastRequest 取到某接口的最后一次请求。
func (s *gatewayStub) lastRequest(apiName string) stubRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.reqs) - 1; i >= 0; i-- {
		if apiNameFromPath(s.reqs[i].path) == apiName {
			return s.reqs[i]
		}
	}
	s.t.Fatalf("没有到接口 %q 的请求", apiName)
	return stubRequest{}
}

// fixtureByAPI 默认响应：接口名 → testdata 文件（来源见 testdata/README.md）。
var fixtureByAPI = map[string]string{
	"alibaba.createOrder.preview":                   "preview_response.json",
	"alibaba.trade.fastCreateOrder":                 "create_order_response.json",
	"alibaba.trade.get.buyerView":                   "get_order_response.json",
	"alibaba.trade.getBuyerOrderList":               "list_orders_response.json",
	"alibaba.trade.getLogisticsInfos.buyerView":     "logistics_infos_response.json",
	"alibaba.trade.getLogisticsTraceInfo.buyerView": "logistics_trace_response.json",
	"getToken": "get_token_response.json",
}

// fakeClock 可控时钟。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// testEnv 一整套测试装配：假网关 + 内存凭据 + 客户端。
type testEnv struct {
	client *Client
	creds  *fakeCreds
	stub   *gatewayStub
	clock  *fakeClock
}

func newTestEnv(t *testing.T, configure func(creds *fakeCreds, stub *gatewayStub)) *testEnv {
	t.Helper()
	creds := newFakeCreds()
	creds.payloads[kindAlibabaApp] = map[string]string{"app_key": "1234567", "app_secret": testAppSecret}
	creds.payloads[kindAlibabaToken] = map[string]string{"access_token": "old-access", "refresh_token": "old-refresh"}

	stub := newGatewayStub(t)
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)

	if configure != nil {
		configure(creds, stub)
	}

	clock := &fakeClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	client, err := NewClient(Config{
		BaseURL:     srv.URL + "/openapi",
		Credentials: creds,
		Registry:    ratelimit.New(ratelimit.Config{}, 2, time.Millisecond),
		Now:         clock.Now,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return &testEnv{client: client, creds: creds, stub: stub, clock: clock}
}

func formToMap(v url.Values) map[string]string {
	m := make(map[string]string, len(v))
	for k := range v {
		m[k] = v.Get(k)
	}
	return m
}

// assertSigned 校验请求带上了用 app_secret 算出的正确签名。
func assertSigned(t *testing.T, req stubRequest, namespace, name string) {
	t.Helper()
	sig := req.form.Get(signatureParam)
	if sig == "" {
		t.Fatal("请求没带 _aop_signature")
	}
	want := sign(signPath(1, namespace, name, "1234567"), testAppSecret, formToMap(req.form))
	if sig != want {
		t.Fatalf("请求签名不对\n got: %s\nwant: %s", sig, want)
	}
	if req.form.Get(timestampParam) == "" {
		t.Fatal("请求没带 _aop_timestamp")
	}
}

func testAddress() Address {
	return Address{
		FullName:     "张三",
		Mobile:       "13800138000",
		Phone:        "0571-88888888",
		PostCode:     "310000",
		ProvinceText: "浙江省",
		CityText:     "杭州市",
		AreaText:     "西湖区",
		Address:      "文一西路 969 号",
	}
}

func testCargo() []Cargo {
	return []Cargo{{OfferID: 612345678901, SpecID: "b266e0726506185beaf205cbae88530d", Quantity: 2}}
}

// ---------- 五个方法的录制响应单测（验收第 3 条） ----------

func TestPreviewOrder_Success(t *testing.T) {
	env := newTestEnv(t, nil)

	res, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{
		Address:   testAddress(),
		CargoList: testCargo(),
	})
	if err != nil {
		t.Fatalf("PreviewOrder: %v", err)
	}
	if len(res.OrderPreviews) != 1 || !res.OrderPreviews[0].Status {
		t.Fatalf("预览结果不符合 fixture: %+v", res.OrderPreviews)
	}
	if got := res.OrderPreviews[0].SumPayment; got != 4400 {
		t.Fatalf("sumPayment = %d, want 4400", got)
	}
	if got := res.OrderPreviews[0].CargoList[0].FinalUnitPrice; got != 18 {
		t.Fatalf("finalUnitPrice = %d, want 18", got)
	}

	req := env.stub.lastRequest("alibaba.createOrder.preview")
	if got := req.form.Get("flow"); got != "general" {
		t.Fatalf("flow = %q, want general", got)
	}
	if got := req.form.Get(tokenParam); got != "6100newtokennewtokennewtoken0000" {
		t.Fatalf("access_token = %q（应为续期后的新 token）", got)
	}
	var gotAddr map[string]string
	if err := json.Unmarshal([]byte(req.form.Get("addressParam")), &gotAddr); err != nil {
		t.Fatalf("addressParam 不是 JSON: %v", err)
	}
	if gotAddr["fullName"] != "张三" || gotAddr["mobile"] != "13800138000" {
		t.Fatalf("addressParam 内容不对: %v", gotAddr)
	}
	var gotCargo []map[string]any
	if err := json.Unmarshal([]byte(req.form.Get("cargoParamList")), &gotCargo); err != nil {
		t.Fatalf("cargoParamList 不是 JSON 数组: %v", err)
	}
	if len(gotCargo) != 1 || gotCargo[0]["specId"] != "b266e0726506185beaf205cbae88530d" {
		t.Fatalf("cargoParamList 内容不对: %v", gotCargo)
	}
	assertSigned(t, req, "com.alibaba.trade", "alibaba.createOrder.preview")
}

func TestCreateOrder_Success(t *testing.T) {
	env := newTestEnv(t, nil)

	res, err := env.client.CreateOrder(context.Background(), CreateOrderRequest{
		Address:   testAddress(),
		CargoList: testCargo(),
		Message:   "OZON-100",
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if got := res.OrderID.String(); got != "58218860983545944" {
		t.Fatalf("orderId = %q", got)
	}
	if res.TotalSuccessAmount != 4400 || res.PostFee != 800 {
		t.Fatalf("金额不对: %+v", res)
	}

	req := env.stub.lastRequest("alibaba.trade.fastCreateOrder")
	if got := req.form.Get("message"); got != "OZON-100" {
		t.Fatalf("message = %q, want OZON-100", got)
	}
	assertSigned(t, req, "com.alibaba.trade", "alibaba.trade.fastCreateOrder")
}

func TestGetOrder_Success(t *testing.T) {
	env := newTestEnv(t, nil)

	order, err := env.client.GetOrder(context.Background(), 58218860983545944)
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if order.BaseInfo.Status != "waitbuyerreceive" {
		t.Fatalf("status = %q", order.BaseInfo.Status)
	}
	if got := order.BaseInfo.TotalAmount.String(); got != "6.15" {
		t.Fatalf("totalAmount = %s, want 6.15（元）", got)
	}
	if got := order.BaseInfo.SumProductPayment.String(); got != "0.3" {
		t.Fatalf("sumProductPayment = %s", got)
	}
	if len(order.ProductItems) != 3 {
		t.Fatalf("productItems = %d 条", len(order.ProductItems))
	}
	if got := order.ProductItems[0].ProductID.String(); got != "547486647009" {
		t.Fatalf("productID = %q", got)
	}
	if got := order.TradeTerms[0].PhasAmount.String(); got != "6.15" {
		t.Fatalf("tradeTerms[0].phasAmount = %s", got)
	}
	if !order.BaseInfo.CreateTime.Parsed() || order.BaseInfo.CreateTime.Year() != 2017 {
		t.Fatalf("createTime 没解析出来: %q", order.BaseInfo.CreateTime.Raw)
	}
	if got := order.BaseInfo.IDOfStr.String(); got != "58218860983545941" {
		t.Fatalf("idOfStr = %q", got)
	}
	if order.NativeLogistics == nil || order.NativeLogistics.City != "杭州市" {
		t.Fatalf("nativeLogistics 没解析出来: %+v", order.NativeLogistics)
	}

	req := env.stub.lastRequest("alibaba.trade.get.buyerView")
	if got := req.form.Get("orderId"); got != "58218860983545944" {
		t.Fatalf("orderId = %q", got)
	}
	if got := req.form.Get("webSite"); got != "1688" {
		t.Fatalf("webSite = %q", got)
	}
	assertSigned(t, req, "com.alibaba.trade", "alibaba.trade.get.buyerView")
}

func TestListBuyerOrders_Success(t *testing.T) {
	env := newTestEnv(t, nil)

	res, err := env.client.ListBuyerOrders(context.Background(), ListBuyerOrdersRequest{
		CreateStartTime: time.Date(2018, 8, 1, 0, 0, 0, 0, time.UTC),
		CreateEndTime:   time.Date(2018, 8, 31, 23, 59, 59, 0, time.UTC),
		OrderStatus:     "cancel",
	})
	if err != nil {
		t.Fatalf("ListBuyerOrders: %v", err)
	}
	if res.TotalRecord != 537 {
		t.Fatalf("totalRecord = %d", res.TotalRecord)
	}
	if len(res.Orders) != 1 || res.Orders[0].BaseInfo.Status != "cancel" {
		t.Fatalf("订单列表内容不对: %+v", res.Orders)
	}
	if got := res.Orders[0].BaseInfo.CloseReason; got != "BUYER_NO_PAY" {
		t.Fatalf("closeReason = %q", got)
	}

	req := env.stub.lastRequest("alibaba.trade.getBuyerOrderList")
	if got := req.form.Get("createStartTime"); got != "2018-08-01 00:00:00" {
		t.Fatalf("createStartTime = %q", got)
	}
	if got := req.form.Get("page"); got != "1" {
		t.Fatalf("page = %q", got)
	}
	if got := req.form.Get("pageSize"); got != "50" {
		t.Fatalf("pageSize = %q", got)
	}
	if _, ok := req.form["isHis"]; ok {
		t.Fatal("isHis=false 不应发送")
	}
	assertSigned(t, req, "com.alibaba.trade", "alibaba.trade.getBuyerOrderList")
}

func TestGetLogistics_Success(t *testing.T) {
	env := newTestEnv(t, nil)

	lg, err := env.client.GetLogistics(context.Background(), 188983797838441800)
	if err != nil {
		t.Fatalf("GetLogistics: %v", err)
	}
	if len(lg.Infos) != 1 {
		t.Fatalf("infos = %d 条", len(lg.Infos))
	}
	if lg.Infos[0].Status != "SIGN" || lg.Infos[0].LogisticsID != "BX111841674232006" {
		t.Fatalf("infos[0] = %+v", lg.Infos[0])
	}
	if len(lg.Infos[0].SendGoods) != 1 || lg.Infos[0].SendGoods[0].Quantity != 2 {
		t.Fatalf("sendGoods = %+v", lg.Infos[0].SendGoods)
	}
	if lg.Infos[0].Sender == nil || lg.Infos[0].Sender.SenderName != "张三" {
		t.Fatalf("sender = %+v", lg.Infos[0].Sender)
	}

	if len(lg.Traces) != 1 {
		t.Fatalf("traces = %d 条", len(lg.Traces))
	}
	if lg.Traces[0].LogisticsBillNo != "3832890717253" {
		t.Fatalf("logisticsBillNo = %q（国内段单号）", lg.Traces[0].LogisticsBillNo)
	}
	if len(lg.Traces[0].Steps) != 9 {
		t.Fatalf("轨迹 %d 步", len(lg.Traces[0].Steps))
	}
	if got := lg.Traces[0].Steps[0].AcceptTime; got != "2018-07-24 21:55:33" {
		t.Fatalf("acceptTime = %q", got)
	}

	for _, name := range []string{"alibaba.trade.getLogisticsInfos.buyerView", "alibaba.trade.getLogisticsTraceInfo.buyerView"} {
		assertSigned(t, env.stub.lastRequest(name), "com.alibaba.logistics", name)
	}
}

// TestGetLogistics_NoTrace 卖家未发货：官方两种「没有轨迹」写法都翻成空轨迹、不报错。
func TestGetLogistics_NoTrace(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"errorMessage 文案（官方样例）", `{"errorMessage":"该订单没有物流跟踪信息。","success":true}`},
		{"errorCode 404（官方文档错误码）", `{"errorCode":"404","errorMessage":"没有物流跟踪信息"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
				stub.handler = func(w http.ResponseWriter, r *http.Request, apiName string, _ int) {
					w.Header().Set("Content-Type", "application/json")
					switch apiName {
					case "alibaba.trade.getLogisticsInfos.buyerView":
						_, _ = w.Write([]byte(`{"result":[],"success":true}`))
					case "alibaba.trade.getLogisticsTraceInfo.buyerView":
						_, _ = w.Write([]byte(tc.body))
					default:
						b, _ := os.ReadFile(filepath.Join("testdata", fixtureByAPI[apiName]))
						_, _ = w.Write(b)
					}
				}
			})

			lg, err := env.client.GetLogistics(context.Background(), 1)
			if err != nil {
				t.Fatalf("未发货不应报错: %v", err)
			}
			if len(lg.Traces) != 0 || len(lg.Infos) != 0 {
				t.Fatalf("应为空: %+v", lg)
			}
		})
	}
}
