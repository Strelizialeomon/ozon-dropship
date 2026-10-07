package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/alibaba"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/order"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/ozon"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/purchase"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/shipment"

	"github.com/shopspring/decimal"
)

// ---- 纯映射 ----

func TestPostingFromListAndDetail(t *testing.T) {
	deadline := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	list := ozon.Posting{
		PostingNumber:       "0123-1",
		OrderNumber:         "ORD-1",
		ParentPostingNumber: "0123-0",
		Status:              order.OzonAwaitingPackaging,
		Substatus:           "posting_awaiting_passport_data",
		TplIntegrationType:  order.TplOzon,
		ShipmentDate:        ozon.Time{Time: deadline},
		Products: []ozon.ListProduct{
			{OfferID: "SKU-A", Quantity: 2, Price: &ozon.Money{Amount: "10.50", Currency: "CNY"}},
			{OfferID: "SKU-B", Quantity: 1, Price: &ozon.Money{Amount: "3.00", Currency: "CNY"}},
		},
	}
	p := postingFromList(&list)
	if p.PostingNumber != "0123-1" || p.Status != order.OzonAwaitingPackaging ||
		p.ParentPostingNumber != "0123-0" || p.TplIntegrationType != order.TplOzon {
		t.Fatalf("标量字段没映射对: %+v", p)
	}
	if p.ShipDeadline == nil || !p.ShipDeadline.Equal(deadline) {
		t.Fatalf("发货截止没映射对: %v", p.ShipDeadline)
	}
	if len(p.Items) != 2 || p.Items[0].OzonOfferID != "SKU-A" || p.Items[0].Qty != 2 {
		t.Fatalf("商品行没映射对: %+v", p.Items)
	}
	if !p.Items[0].Price.Equal(decimal.RequireFromString("10.50")) || p.Currency != "CNY" {
		t.Fatalf("价格/币种没映射对: %+v currency=%s", p.Items[0], p.Currency)
	}
	if !p.TotalAmount().Equal(decimal.RequireFromString("24.00")) {
		t.Fatalf("总额 = %s，期望 24.00", p.TotalAmount())
	}

	// 详情路径：价格是字符串、币种单独一列。
	detail := ozon.PostingDetail{
		PostingNumber:      "0123-2",
		Status:             order.OzonAwaitingDeliver,
		TplIntegrationType: order.Tpl3PLTracking,
		Products: []ozon.DetailProduct{
			{OfferID: "SKU-C", Quantity: 3, Price: "7.25", CurrencyCode: "CNY"},
		},
	}
	d := postingFromDetail(&detail)
	if d.ShipDeadline != nil {
		t.Fatalf("详情没有 shipment_date 时应为 nil，实际 %v", d.ShipDeadline)
	}
	if len(d.Items) != 1 || !d.Items[0].Price.Equal(decimal.RequireFromString("7.25")) {
		t.Fatalf("详情商品行没映射对: %+v", d.Items)
	}
}

func TestSplitChineseAddress(t *testing.T) {
	cases := []struct {
		in                           string
		province, city, area, detail string
	}{
		{"广东省深圳市宝安区航城大道 1 号 3 楼", "广东省", "深圳市", "宝安区", "航城大道 1 号 3 楼"},
		{"浙江省义乌市北苑街道 88 号", "浙江省", "义乌市", "", "北苑街道 88 号"},
		{"深圳市宝安区西乡大道 5 号", "", "深圳市", "宝安区", "西乡大道 5 号"},
		{"上海市浦东新区张江路 100 号", "上海市", "上海市", "浦东新区", "张江路 100 号"},
		{"新疆维吾尔自治区乌鲁木齐市天山区人民路 2 号", "新疆维吾尔自治区", "乌鲁木齐市", "天山区", "人民路 2 号"},
		{"某个没有行政词的长地址", "", "", "", "某个没有行政词的长地址"},
	}
	for _, tc := range cases {
		p, c, a, d := splitChineseAddress(tc.in)
		if p != tc.province || c != tc.city || a != tc.area || d != tc.detail {
			t.Errorf("splitChineseAddress(%q) = (%q, %q, %q, %q)，期望 (%q, %q, %q, %q)",
				tc.in, p, c, a, d, tc.province, tc.city, tc.area, tc.detail)
		}
	}
}

func TestPreviewReasonMapping(t *testing.T) {
	cases := []struct{ code, msg, want string }{
		{"ADDRESS_INVALID", "收货地址不合法", purchase.PreviewReasonAddressInvalid},
		{"", "该商家为首次交易，请人工下单", purchase.PreviewReasonNewSeller},
		{"QUOTA_LIMIT", "今日调用超限", purchase.PreviewReasonOther},
	}
	for _, tc := range cases {
		if got := previewReason(tc.code, tc.msg); got != tc.want {
			t.Errorf("previewReason(%q, %q) = %q，期望 %q", tc.code, tc.msg, got, tc.want)
		}
	}
}

func TestCentsToYuan(t *testing.T) {
	if got := centsToYuan(12345); !got.Equal(decimal.RequireFromString("123.45")) {
		t.Fatalf("12345 分应 = 123.45 元，实际 %s", got)
	}
	if got := centsToYuan(1); !got.Equal(decimal.RequireFromString("0.01")) {
		t.Fatalf("1 分应 = 0.01 元，实际 %s", got)
	}
}

// ---- Ozon：拉单适配（分页取全 + 字段）----

func newTestLimiter() *ratelimit.Registry {
	return ratelimit.New(ratelimit.Config{
		Ozon:    ratelimit.SubjectConfig{DefaultRPS: 50},
		Alibaba: ratelimit.SubjectConfig{DefaultRPS: 50},
	}, 3, time.Second)
}

func TestOzonPostingSourcePagingAndMapping(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4/posting/fbs/list":
			var body struct {
				Cursor string `json:"cursor"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			calls++
			w.Header().Set("Content-Type", "application/json")
			// ⚠️ v4 与 v3 不同：cursor / has_next / postings 在**顶层**，不裹 result
			//（官方 v4 schema；v3 才裹，B 的 fixture 同此口径）。
			if body.Cursor == "" {
				_, _ = w.Write([]byte(`{"postings":[{"posting_number":"p1","status":"awaiting_packaging","products":[{"offer_id":"SKU-1","quantity":1,"price":{"amount":"10.00","currency":"CNY"}}]}],"has_next":true,"cursor":"next-1"}`))
				return
			}
			if body.Cursor != "next-1" {
				t.Errorf("第二页 cursor 应回传 next-1，实际 %q", body.Cursor)
			}
			_, _ = w.Write([]byte(`{"postings":[{"posting_number":"p2","status":"awaiting_deliver","products":[{"offer_id":"SKU-2","quantity":2,"price":{"amount":"5.00","currency":"CNY"}}]}],"has_next":false}`))
		default:
			t.Errorf("不该请求 %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	src := &ozonPostingSource{c: ozon.New(newTestLimiter(), ozon.Options{
		ClientID: "c1", APIKey: "k1", BaseURL: srv.URL,
	})}
	got, err := src.ListPostings(context.Background(), order.ListPostingsRequest{
		Since: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(got) != 2 || got[0].PostingNumber != "p1" || got[1].PostingNumber != "p2" {
		t.Fatalf("应翻页取全两单（calls=%d, got=%+v）", calls, got)
	}
}

// ---- Ozon：备货按详情商品组装单包裹 ----

func TestOzonFulfillerShipPostingBuildsPackage(t *testing.T) {
	var shipped bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v3/posting/fbs/get":
			_, _ = w.Write([]byte(`{"result":{"posting_number":"p1","status":"awaiting_deliver","products":[{"offer_id":"SKU-1","sku":111,"quantity":2},{"offer_id":"SKU-2","sku":222,"quantity":1}]}}`))
		case "/v4/posting/fbs/ship":
			var body struct {
				PostingNumber string `json:"posting_number"`
				Packages      []struct {
					Products []struct {
						ProductID int64 `json:"product_id"`
						Quantity  int32 `json:"quantity"`
					} `json:"products"`
				} `json:"packages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.PostingNumber != "p1" || len(body.Packages) != 1 || len(body.Packages[0].Products) != 2 {
				t.Errorf("组装请求不对: %+v", body)
			}
			if body.Packages[0].Products[0].ProductID != 111 || body.Packages[0].Products[0].Quantity != 2 {
				t.Errorf("商品行不对: %+v", body.Packages[0].Products)
			}
			shipped = true
			_, _ = w.Write([]byte(`{"result":["p1"]}`))
		default:
			t.Errorf("不该请求 %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	f := &ozonFulfiller{c: ozon.New(newTestLimiter(), ozon.Options{ClientID: "c1", APIKey: "k1", BaseURL: srv.URL})}
	if err := f.ShipPosting(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}
	if !shipped {
		t.Fatal("没有发备货请求")
	}
	st, err := f.GetPosting(context.Background(), "p1")
	if err != nil || st.Status != "awaiting_deliver" {
		t.Fatalf("复核状态失败: %+v %v", st, err)
	}
}

// ---- Ozon：面单两步流程（create → 轮询 get → 下载 file_url）----

func TestOzonFulfillerLabelTwoStep(t *testing.T) {
	var got int
	file := []byte("%PDF-1.4 fake-label")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/posting/fbs/package-label/create":
			var body struct {
				PostingNumbers []string `json:"posting_numbers"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.PostingNumbers) != 1 || body.PostingNumbers[0] != "p1" {
				t.Errorf("创建任务请求不对: %+v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tasks":[{"task_id":42,"task_type":"package_label"}]}`))
		case "/v2/posting/fbs/package-label/get":
			got++
			w.Header().Set("Content-Type", "application/json")
			if got == 1 {
				// 第一次还没生成好 → 适配器应等待后重试。
				_, _ = w.Write([]byte(`{"status":{"code":"in_progress","postings_count":1}}`))
				return
			}
			_, _ = w.Write([]byte(`{"file_url":"` + "https://" + r.Host + `/label-file.pdf","status":{"code":"completed","postings_count":1,"printed_postings_count":1}}`))
		case "/label-file.pdf":
			_, _ = w.Write(file)
		default:
			t.Errorf("不该请求 %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	// TLS 测试服是自签证书：换默认 Transport 才连得上（生产用默认 Transport）。
	oldTransport := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = oldTransport }()

	f := &ozonFulfiller{
		c:             ozon.New(newTestLimiter(), ozon.Options{ClientID: "c1", APIKey: "k1", BaseURL: srv.URL}),
		labelTimeout:  5 * time.Second,
		labelInterval: 10 * time.Millisecond,
		download:      downloadURL,
	}
	pdf, err := f.GetPackageLabel(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("应轮询两次（先 in_progress 再 completed），实际 %d", got)
	}
	if string(pdf) != string(file) {
		t.Fatalf("下载的面单内容不对: %q", pdf)
	}
}

// ---- 1688：下单带留言与外部单号；预览映射地址与结果 ----

type stubCredStore struct {
	mu       sync.Mutex
	payloads map[string]map[string]string
	expires  map[string]*time.Time
}

func newStubCredStore() *stubCredStore {
	future := time.Now().Add(5 * time.Hour)
	return &stubCredStore{
		payloads: map[string]map[string]string{
			"alibaba_app":   {"app_key": "12345", "app_secret": "secret"},
			"alibaba_token": {"access_token": "tok-1234567890", "refresh_token": "ref-1"},
		},
		expires: map[string]*time.Time{"alibaba_token": &future},
	}
}

func (s *stubCredStore) Get(_ context.Context, _, kind string) (map[string]string, *time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.payloads[kind], s.expires[kind], nil
}

func (s *stubCredStore) Put(_ context.Context, _, kind string, payload map[string]string, expiresAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payloads[kind] = payload
	s.expires[kind] = expiresAt
	return nil
}

func (s *stubCredStore) SetExpiry(_ context.Context, _, kind string, expiresAt, _ *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expires[kind] = expiresAt
	return nil
}

func TestAlibabaBuyerPreviewAndCreate(t *testing.T) {
	// 1688 网关是表单编码（application/x-www-form-urlencoded），复杂参数是 JSON 字符串值。
	var previewForm, createForm map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "getToken"):
			// C 的 token 管理器冷启动必然续期一次（其包注释写明这是刻意选择）。
			_, _ = w.Write([]byte(`{"access_token":"tok-new-1234567890","refresh_token":"ref-new-1","expires_in":36000,"refresh_token_timeout":"20270507120000+0800"}`))
		case strings.Contains(r.URL.Path, "fastCreateOrder"):
			_ = r.ParseForm()
			createForm = r.PostForm
			_, _ = w.Write([]byte(`{"result":{"success":true,"orderId":"99887766","totalSuccessAmount":12345,"postFee":0,"failedOfferList":[]}}`))
		case strings.Contains(r.URL.Path, "preview"):
			_ = r.ParseForm()
			previewForm = r.PostForm
			// 官方字段名就是 orderPreviewResuslt（官方原文拼写如此，不是笔误）。
			_, _ = w.Write([]byte(`{"success":true,"orderPreviewResuslt":[{"status":true,"sumPayment":12345,"sumCarriage":800}]}`))
		default:
			t.Errorf("不该请求 %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client, err := alibaba.NewClient(alibaba.Config{
		BaseURL:     srv.URL,
		Registry:    newTestLimiter(),
		Credentials: newStubCredStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	b := &alibabaBuyer{c: client}

	// 预览：地址要拆成省市区分段、金额单位是分。
	pv, err := b.PreviewOrder(context.Background(), purchase.PreviewRequest{
		ItemID: "6688990011", SkuID: "spec-1", Qty: 2,
		Address: purchase.Address{Name: "深圳仓", Phone: "13800000000", Address: "广东省深圳市宝安区航城大道 1 号"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pv.CanOrder || !pv.Freight.Equal(decimal.RequireFromString("8.00")) {
		t.Fatalf("预览结果不对: %+v", pv)
	}
	var addr struct {
		ProvinceText string `json:"provinceText"`
		CityText     string `json:"cityText"`
		AreaText     string `json:"areaText"`
		Address      string `json:"address"`
	}
	if err := json.Unmarshal([]byte(previewForm["addressParam"][0]), &addr); err != nil {
		t.Fatalf("请求里的 addressParam 不是 JSON: %v", err)
	}
	if addr.ProvinceText != "广东省" || addr.CityText != "深圳市" || addr.AreaText != "宝安区" || addr.Address == "" {
		t.Fatalf("地址没拆对: %+v", addr)
	}

	// 下单：留言与外部单号都要带上（中转点认包 + 防重核对）。
	bo, err := b.CreateOrder(context.Background(), purchase.CreateOrderRequest{
		ItemID: "6688990011", SkuID: "spec-1", Qty: 2,
		Address:    purchase.Address{Name: "深圳仓", Phone: "13800000000", Address: "广东省深圳市宝安区航城大道 1 号"},
		Remark:     "0123-1#T12345678",
		OutOrderID: "0123-1#T12345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bo.PlatformOrderID != "99887766" || !bo.Amount.Equal(decimal.RequireFromString("123.45")) {
		t.Fatalf("下单结果映射不对: %+v", bo)
	}
	if createForm["message"][0] != "0123-1#T12345678" || createForm["outOrderId"][0] != "0123-1#T12345678" {
		t.Fatalf("留言/外部单号没带上: %+v", createForm)
	}
}

// 部分商品下单失败不能被当成功吞掉（1688 整体 success 仍为 true 的形态）。
func TestAlibabaBuyerCreateOrderPartialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "getToken") {
			_, _ = w.Write([]byte(`{"access_token":"tok-new-1234567890","refresh_token":"ref-new-1","expires_in":36000,"refresh_token_timeout":"20270507120000+0800"}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":{"success":true,"orderId":"99887766","totalSuccessAmount":100,"failedOfferList":[{"offerId":"6688990011","errorCode":"OFFER_INVALID","errorMessage":"商品已下架"}]}}`))
	}))
	defer srv.Close()

	client, err := alibaba.NewClient(alibaba.Config{BaseURL: srv.URL, Registry: newTestLimiter(), Credentials: newStubCredStore()})
	if err != nil {
		t.Fatal(err)
	}
	b := &alibabaBuyer{c: client}
	_, err = b.CreateOrder(context.Background(), purchase.CreateOrderRequest{
		ItemID: "6688990011", SkuID: "s", Qty: 1, Remark: "x", OutOrderID: "x",
	})
	if err == nil {
		t.Fatal("部分商品失败必须报错，不能假装下单成功")
	}
	// 而且要标成「不可重试」：重试会被核对路径洗成已下单（轻审 #6）。
	if !errors.Is(err, purchase.ErrOrderIncomplete) {
		t.Fatalf("应包成 ErrOrderIncomplete，实际: %v", err)
	}
}

// 面单任务出错要报出来（不能一直等到超时）。
func TestOzonFulfillerLabelTaskError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v3/posting/fbs/package-label/create":
			_, _ = w.Write([]byte(`{"tasks":[{"task_id":7}]}`))
		default:
			_, _ = w.Write([]byte(`{"status":{"code":"error"},"error":{"code":"INVALID","message":"寄件号不存在"}}`))
		}
	}))
	defer srv.Close()

	f := &ozonFulfiller{
		c:             ozon.New(newTestLimiter(), ozon.Options{ClientID: "c1", APIKey: "k1", BaseURL: srv.URL}),
		labelTimeout:  time.Second,
		labelInterval: 5 * time.Millisecond,
		download:      downloadURL,
	}
	if _, err := f.GetPackageLabel(context.Background(), "p1"); err == nil {
		t.Fatal("任务 error 应直接报错")
	}
}

// 传单号被 Ozon 拒绝（逐条 result=false）要报出来。
func TestOzonFulfillerSetTrackingRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"posting_number":"p1","result":false,"error":"tracking number already set"}]}`))
	}))
	defer srv.Close()

	f := &ozonFulfiller{c: ozon.New(newTestLimiter(), ozon.Options{ClientID: "c1", APIKey: "k1", BaseURL: srv.URL})}
	if err := f.SetTrackingNumber(context.Background(), "p1", "SF123456", "顺丰"); err == nil {
		t.Fatal("被拒绝的单号应报错")
	}
}

// 预留：确认适配器满足 D 的三个接口（编译期断言）。
var (
	_ order.PostingSource       = (*ozonPostingSource)(nil)
	_ shipment.PostingFulfiller = (*ozonFulfiller)(nil)
	_ purchase.BuyerClient      = (*alibabaBuyer)(nil)
	_ alibaba.CredentialStore   = credentialStoreAdapter{}
)

// ---- PR #19 轻审的回归用例 ----

// #3：面单任务 completed 但本单没印出来（unprinted_postings 非空）→ 报错，不许当成功。
func TestOzonFulfillerLabelUnprintedPostingFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v3/posting/fbs/package-label/create":
			_, _ = w.Write([]byte(`{"tasks":[{"task_id":9}]}`))
		case "/v2/posting/fbs/package-label/get":
			_, _ = w.Write([]byte(`{"file_url":"https://example.invalid/x.pdf","status":{"code":"completed","postings_count":2,"printed_postings_count":1,"unprinted_postings":[{"posting_number":"p1","message":"Отправление не готово к отгрузке"}]}}`))
		default:
			t.Error("不该下载：本单没印出来时不能当成功", r.URL.Path)
		}
	}))
	defer srv.Close()

	f := &ozonFulfiller{
		c:             ozon.New(newTestLimiter(), ozon.Options{ClientID: "c1", APIKey: "k1", BaseURL: srv.URL}),
		labelTimeout:  time.Second,
		labelInterval: 5 * time.Millisecond,
		download:      downloadURL,
	}
	if _, err := f.GetPackageLabel(context.Background(), "p1"); err == nil {
		t.Fatal("completed 但 unprinted 非空必须报错")
	}
}

// #4：逐条结果为空 / 不是本寄件号 → 视为没传成功。
func TestOzonFulfillerSetTrackingNeedsMatchingSuccess(t *testing.T) {
	cases := []struct{ name, body string }{
		{"空结果数组", `{"result":[]}`},
		{"返回别的寄件号", `{"result":[{"posting_number":"other","result":true}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			f := &ozonFulfiller{c: ozon.New(newTestLimiter(), ozon.Options{ClientID: "c1", APIKey: "k1", BaseURL: srv.URL})}
			if err := f.SetTrackingNumber(context.Background(), "p1", "SF123456", "顺丰"); err == nil {
				t.Fatal("没有明确 success 就该报错")
			}
		})
	}
}

// #8：面单文件地址只认 https；超限报错不截断。
func TestDownloadURLGuards(t *testing.T) {
	if _, err := downloadURL(context.Background(), "http://169.254.169.254/latest/meta-data/"); err == nil {
		t.Fatal("非 https 地址必须拒绝（SSRF 面）")
	}
	if _, err := downloadURL(context.Background(), "not a url"); err == nil {
		t.Fatal("坏地址应报错")
	}

	// 超限：返回 maxLabelBytes+1 字节 → 必须报错，不许截断当成功。
	big := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := make([]byte, 1<<20)
		for i := 0; i < (maxLabelBytes>>20)+1; i++ {
			_, _ = w.Write(chunk)
		}
	}))
	defer big.Close()
	// 测试服务器是自签证书：换掉默认 Transport 才连得上（生产用默认 Transport）。
	old := http.DefaultTransport
	http.DefaultTransport = big.Client().Transport
	defer func() { http.DefaultTransport = old }()
	if _, err := downloadURL(context.Background(), big.URL+"/big.pdf"); err == nil {
		t.Fatal("超过上限应报错")
	}
}

// #5：买家订单核对要翻页取全（50 一页，第二页的才是本任务的单）。
func TestAlibabaBuyerListBuyerOrdersPaging(t *testing.T) {
	var pages []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "getToken") {
			_, _ = w.Write([]byte(`{"access_token":"tok-new-1234567890","refresh_token":"ref-new-1","expires_in":36000,"refresh_token_timeout":"20270507120000+0800"}`))
			return
		}
		_ = r.ParseForm()
		page, _ := strconv.Atoi(r.PostForm.Get("page"))
		pages = append(pages, page)
		if page == 1 {
			orders := make([]string, 0, 50)
			for i := 0; i < 50; i++ {
				orders = append(orders, `{"baseInfo":{"id":"1`+strconv.Itoa(i)+`","status":"waitbuyerpay"}}`)
			}
			_, _ = w.Write([]byte(`{"result":[` + strings.Join(orders, ",") + `],"totalRecord":51}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":[{"baseInfo":{"id":"999","status":"waitbuyersend","outOrderId":"0123-1#T99999999"}}],"totalRecord":51}`))
	}))
	defer srv.Close()

	client, err := alibaba.NewClient(alibaba.Config{BaseURL: srv.URL, Registry: newTestLimiter(), Credentials: newStubCredStore()})
	if err != nil {
		t.Fatal(err)
	}
	b := &alibabaBuyer{c: client}
	got, err := b.ListBuyerOrders(context.Background(), purchase.ListBuyerOrdersRequest{
		Since: time.Now().Add(-2 * time.Hour), To: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 51 {
		t.Fatalf("应翻页取全 51 单，实际 %d（页码序列 %v）", len(got), pages)
	}
	if got[50].OutOrderID != "0123-1#T99999999" {
		t.Fatalf("第二页的单没取到: %+v", got[50])
	}
}

// #2：核对窗口按北京时间发给网关（不是 UTC 墙钟）。
func TestAlibabaBuyerListBuyerOrdersSendsBeijingTime(t *testing.T) {
	var gotStart string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "getToken") {
			_, _ = w.Write([]byte(`{"access_token":"tok-new-1234567890","refresh_token":"ref-new-1","expires_in":36000,"refresh_token_timeout":"20270507120000+0800"}`))
			return
		}
		_ = r.ParseForm()
		gotStart = r.PostForm.Get("createStartTime")
		_, _ = w.Write([]byte(`{"result":[],"totalRecord":0}`))
	}))
	defer srv.Close()

	client, err := alibaba.NewClient(alibaba.Config{BaseURL: srv.URL, Registry: newTestLimiter(), Credentials: newStubCredStore()})
	if err != nil {
		t.Fatal(err)
	}
	b := &alibabaBuyer{c: client}
	// 2026-10-07 00:30 UTC = 北京时间 08:30 → 网关该收到 08:30 而不是 00:30。
	utc := time.Date(2026, 10, 7, 0, 30, 0, 0, time.UTC)
	if _, err := b.ListBuyerOrders(context.Background(), purchase.ListBuyerOrdersRequest{Since: utc, To: utc.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if gotStart != "2026-10-07 08:30:00" {
		t.Fatalf("应发北京时间，实际 %q", gotStart)
	}
}

// #7：记账金额取实付（tradeTerms），没有付款记录时退回应付款。
func TestBuyerOrderAmountPrefersPaid(t *testing.T) {
	o := &alibaba.Order{}
	o.BaseInfo.ID = "123"
	o.BaseInfo.TotalAmount = decimal.RequireFromString("100.00")
	if got := orderAmount(o); !got.Equal(decimal.RequireFromString("100.00")) {
		t.Fatalf("没有 tradeTerms 时应退回应付款，实际 %s", got)
	}
	o.TradeTerms = []alibaba.TradeTerm{{PhasAmount: decimal.RequireFromString("88.00")}}
	if got := orderAmount(o); !got.Equal(decimal.RequireFromString("88.00")) {
		t.Fatalf("有 tradeTerms 时应取实付，实际 %s", got)
	}
}
