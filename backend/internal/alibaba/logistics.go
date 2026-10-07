package alibaba

import (
	"context"
	"strconv"
	"strings"
)

// Logistics 买家视角物流汇总（国内段：货源 → 中转点）。
// 由两个接口拼成：Infos 给单号/公司/状态，Traces 给轨迹步骤。
type Logistics struct {
	// Infos 物流单信息（alibaba.trade.getLogisticsInfos.buyerView）。
	Infos []LogisticsInfo
	// Traces 物流跟踪（alibaba.trade.getLogisticsTraceInfo.buyerView）；
	// 卖家还没发货时为空——这是正常状态，不算错误。
	Traces []LogisticsTrace
}

// LogisticsInfo 一条物流单信息。
type LogisticsInfo struct {
	// LogisticsID 物流信息 id（1688 侧编号，如 BX111841674232006）。
	LogisticsID string `json:"logisticsId"`
	// LogisticsBillNo 物流单号/运单号（快递公司面单号）。
	LogisticsBillNo string `json:"logisticsBillNo"`
	// OrderEntryIDs 订单号列表（无子订单时 = 主订单号）。
	OrderEntryIDs string `json:"orderEntryIds"`
	// Status 物流状态：WAITACCEPT 未受理 / CANCEL 已撤销 / ACCEPT 已受理 /
	// TRANSPORT 运输中 / NOGET 揽件失败 / SIGN 已签收 ...
	Status string `json:"status"`
	// LogisticsCompanyID / LogisticsCompanyName / LogisticsCompanyNo 物流公司。
	LogisticsCompanyID   string `json:"logisticsCompanyId"`
	LogisticsCompanyName string `json:"logisticsCompanyName"`
	LogisticsCompanyNo   string `json:"logisticsCompanyNo"`
	// Remarks 备注。
	Remarks string `json:"remarks"`
	// SendGoods 物流单里的商品。
	SendGoods []LogisticsSendGood `json:"sendGoods"`
	// Receiver / Sender 收发货人信息（含手机号，属 PII，注意落库口径）。
	Receiver *LogisticsReceiver `json:"receiver"`
	Sender   *LogisticsSender   `json:"sender"`
}

// LogisticsSendGood 物流单里的商品明细。
type LogisticsSendGood struct {
	GoodName string `json:"goodName"`
	Quantity int64  `json:"quantity"`
	Unit     string `json:"unit"`
}

// LogisticsReceiver 收件人信息。
type LogisticsReceiver struct {
	Encrypt              string `json:"encrypt"`
	ReceiverName         string `json:"receiverName"`
	ReceiverMobile       string `json:"receiverMobile"`
	ReceiverPhone        string `json:"receiverPhone"`
	ReceiverProvince     string `json:"receiverProvince"`
	ReceiverCity         string `json:"receiverCity"`
	ReceiverCounty       string `json:"receiverCounty"`
	ReceiverAddress      string `json:"receiverAddress"`
	ReceiverProvinceCode string `json:"receiverProvinceCode"`
	ReceiverCityCode     string `json:"receiverCityCode"`
	ReceiverCountyCode   string `json:"receiverCountyCode"`
}

// LogisticsSender 发件人信息。
type LogisticsSender struct {
	Encrypt            string `json:"encrypt"`
	SenderName         string `json:"senderName"`
	SenderMobile       string `json:"senderMobile"`
	SenderPhone        string `json:"senderPhone"`
	SenderProvince     string `json:"senderProvince"`
	SenderCity         string `json:"senderCity"`
	SenderCounty       string `json:"senderCounty"`
	SenderAddress      string `json:"senderAddress"`
	SenderProvinceCode string `json:"senderProvinceCode"`
	SenderCityCode     string `json:"senderCityCode"`
	SenderCountyCode   string `json:"senderCountyCode"`
}

// LogisticsTrace 一条物流跟踪。
type LogisticsTrace struct {
	// LogisticsID 物流编号（如 BX110096003841234）。
	LogisticsID string `json:"logisticsId"`
	// OrderID 订单编号（大整数 id，字符串语义保全）。
	OrderID FlexString `json:"orderId"`
	// LogisticsBillNo 物流单号（快递面单号）。
	LogisticsBillNo string `json:"logisticsBillNo"`
	// Steps 轨迹步骤（时间正序）。
	Steps []LogisticsStep `json:"logisticsSteps"`
}

// LogisticsStep 一条轨迹（时间格式 "2018-07-24 21:55:33"）。
type LogisticsStep struct {
	AcceptTime string `json:"acceptTime"`
	Remark     string `json:"remark"`
}

// GetLogistics 取买家视角物流：Infos 给单号/状态/公司，Traces 给轨迹。
//
// 失败语义（两个子调用任一失败都整体报错，不返回「部分成功」的结果）：
//   - Infos 调用失败（订单不存在、无权限等）→ 返回错误；
//   - 轨迹调用失败 → 同样返回错误，调用方重试整次调用。
//     唯一例外是「没有物流跟踪信息」（卖家未发货的正常状态，官方文档 code 404 /
//     样例 errorMessage 文案）——翻成空 Traces、不报错。
//
// 这么选的原因：单号在 Infos 里、轨迹在 Trace 里，调用方（S1-D 回填国内快递号）
// 要的是两者的完整视图；吞掉一半再悄悄返回会让「轨迹查不到」和「轨迹还没生成」
// 在下游分不开。
func (c *Client) GetLogistics(ctx context.Context, orderID uint64) (*Logistics, error) {
	out := &Logistics{}

	err := c.do(ctx, apiLogisticsInfos, map[string]any{
		"webSite": "1688",
		"orderId": strconv.FormatUint(orderID, 10),
	}, true, false, func(body []byte) error {
		var raw struct {
			statusFields
			Result []LogisticsInfo `json:"result"`
		}
		if err := unmarshalBody(apiLogisticsInfos, body, &raw); err != nil {
			return err
		}
		if ae := raw.err(apiLogisticsInfos.name); ae != nil {
			return c.fail(ae)
		}
		out.Infos = raw.Result
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = c.do(ctx, apiLogisticsTrace, map[string]any{
		"webSite": "1688",
		"orderId": strconv.FormatUint(orderID, 10),
	}, true, false, func(body []byte) error {
		var raw struct {
			statusFields
			Traces []LogisticsTrace `json:"logisticsTrace"`
		}
		if err := unmarshalBody(apiLogisticsTrace, body, &raw); err != nil {
			return err
		}
		if ae := raw.err(apiLogisticsTrace.name); ae != nil {
			// 「没有物流跟踪信息」是未发货的正常状态（官方文档给 code 404，
			// 样例给 errorMessage 文案），翻成空轨迹而不是错误。
			if isNoTrace(ae) {
				return nil
			}
			return c.fail(ae)
		}
		out.Traces = raw.Traces
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// isNoTrace 判断错误是否为「还没有物流跟踪信息」。
// 依据：官方文档错误码列表里 404 = 物流信息不存在；出参示例给 errorMessage 文案
// （"该订单没有物流跟踪信息。"）。物流轨迹接口的 404 按此口径理解——如果真机上发现
// 404 还承载别的语义（如订单不存在），按实测结论收窄这里的判断。
func isNoTrace(ae *APIError) bool {
	if strings.EqualFold(ae.Code, "404") {
		return true
	}
	return strings.Contains(ae.Message, "没有物流跟踪信息")
}
