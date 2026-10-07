import com.alibaba.tuna.util.SignatureUtil;

import java.util.LinkedHashMap;
import java.util.Map;
import java.util.TreeMap;

/**
 * 用官方 SDK（alibaba/AliOpen · tuna-java-sdk 的 com.alibaba.tuna.util.SignatureUtil）
 * 为固定参数集生成 _aop_signature 参考值，供 Go 端 sign_test.go 对照。
 *
 * 官方调用方在签名前会把 _aop_signature 从参数中移除（校验端 SimpleHttpProcessorHandler
 * 同样跳过该键），本驱动照此语义：先 remove(_aop_signature) 再调 hmacSha1(path, params, secret)。
 */
public class SignVec {
    static void vec(String name, String path, String secret, Map<String, String> params) {
        Map<String, String> p = new LinkedHashMap<>(params);
        p.remove("_aop_signature");
        // 官方 SignatureUtil.hmacSha1 会对 key+value 拼接串排序；这里也按 key 排一份用于展示
        Map<String, Object> sorted = new TreeMap<>();
        sorted.putAll(p);
        byte[] sig = SignatureUtil.hmacSha1(path, sorted, secret);
        System.out.println("VEC " + name);
        System.out.println("PATH " + path);
        System.out.println("SECRET " + secret);
        for (Map.Entry<String, Object> e : sorted.entrySet()) {
            System.out.println("P " + jsonStr(e.getKey()) + " " + jsonStr(e.getValue().toString()));
        }
        System.out.println("SIG " + SignatureUtil.encodeHexStr(sig));
        System.out.println();
    }

    static String jsonStr(String s) {
        StringBuilder b = new StringBuilder();
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"': b.append("\\\""); break;
                case '\\': b.append("\\\\"); break;
                case '\n': b.append("\\n"); break;
                case '\r': b.append("\\r"); break;
                case '\t': b.append("\\t"); break;
                default:
                    if (c < 0x20) {
                        b.append(String.format("\\u%04x", (int) c));
                    } else {
                        b.append(c);
                    }
            }
        }
        return b.toString();
    }

    public static void main(String[] args) {
        final String secret = "test_app_secret_0123456789abcdef";
        final String appKey = "1234567";

        // V1：fastCreateOrder 全参数（中文、嵌套 JSON）
        Map<String, String> v1 = new LinkedHashMap<>();
        v1.put("access_token", "6100abcdef6100abcdef6100abcdef");
        v1.put("_aop_timestamp", "1728273600000");
        v1.put("flow", "general");
        v1.put("message", "OZON-0001-0001");
        v1.put("addressParam", "{\"fullName\":\"张三\",\"mobile\":\"13800138000\",\"phone\":\"0571-88888888\",\"postCode\":\"310000\",\"provinceText\":\"浙江省\",\"cityText\":\"杭州市\",\"areaText\":\"西湖区\",\"townText\":\"\",\"address\":\"文一西路 969 号\",\"districtCode\":\"\"}");
        v1.put("cargoParamList", "[{\"offerId\":612345678901,\"specId\":\"b266e0726506185beaf205cbae88530d\",\"quantity\":2}]");
        vec("V1-fastCreateOrder-full", "param2/1/com.alibaba.trade/alibaba.trade.fastCreateOrder/" + appKey, secret, v1);

        // V2：预览接口
        Map<String, String> v2 = new LinkedHashMap<>();
        v2.put("access_token", "6100abcdef6100abcdef6100abcdef");
        v2.put("_aop_timestamp", "1728273600000");
        v2.put("flow", "general");
        v2.put("addressParam", "{\"fullName\":\"李四\",\"mobile\":\"13900139000\",\"provinceText\":\"广东省\",\"cityText\":\"深圳市\",\"areaText\":\"南山区\",\"address\":\"科技园路 1 号\"}");
        v2.put("cargoParamList", "[{\"offerId\":612345678902,\"specId\":\"2ba3d63866a71fbae83909d9b4814f01\",\"quantity\":5}]");
        vec("V2-preview", "param2/1/com.alibaba.trade/alibaba.createOrder.preview/" + appKey, secret, v2);

        // V3：订单列表
        Map<String, String> v3 = new LinkedHashMap<>();
        v3.put("access_token", "tok-simple-0001");
        v3.put("createStartTime", "2026-10-01 00:00:00");
        v3.put("createEndTime", "2026-10-01 23:59:59");
        v3.put("isHis", "false");
        v3.put("orderStatus", "waitbuyerreceive");
        v3.put("page", "1");
        v3.put("pageSize", "50");
        v3.put("webSite", "1688");
        vec("V3-orderList", "param2/1/com.alibaba.trade/alibaba.trade.getBuyerOrderList/" + appKey, secret, v3);

        // V4：空值 + _aop_signature 不参与签名
        Map<String, String> v4 = new LinkedHashMap<>();
        v4.put("access_token", "tok-simple-0002");
        v4.put("emptyVal", "");
        v4.put("orderId", "58218860983545944");
        v4.put("fields", "");
        v4.put("webSite", "1688");
        v4.put("_aop_signature", "SHOULD_BE_IGNORED");
        vec("V4-emptyAndExcluded", "param2/1/com.alibaba.logistics/alibaba.trade.getLogisticsInfos.buyerView/" + appKey, secret, v4);

        // V5：特殊字符（&、=、空格、引号、反斜杠）
        Map<String, String> v5 = new LinkedHashMap<>();
        v5.put("access_token", "tok");
        v5.put("message", "备注 & 带=号 还有空格 \"引号\"");
        v5.put("nested", "{\"k\":\"v&v\"}");
        v5.put("path", "a/b\\c");
        vec("V5-specialChars", "param2/1/com.alibaba.trade/alibaba.trade.fastCreateOrder/" + appKey, secret, v5);

        // V6：订单详情
        Map<String, String> v6 = new LinkedHashMap<>();
        v6.put("access_token", "tok-simple-0003");
        v6.put("includeFields", "NativeLogistics");
        v6.put("orderId", "58218860983545944");
        v6.put("webSite", "1688");
        v6.put("_aop_timestamp", "1728273600001");
        vec("V6-getBuyerView", "param2/1/com.alibaba.trade/alibaba.trade.get.buyerView/" + appKey, secret, v6);
    }
}
