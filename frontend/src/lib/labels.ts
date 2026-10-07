// 后端枚举 → 界面中文标签 + 徽标色调。取值以后端 Go 常量 / 总纲 §6 为准，
// 后端枚举加了新值这里要先补——StatusBadge 遇到未知值会显示原文并落到 default 色。

export type Tone = 'default' | 'secondary' | 'destructive' | 'outline' | 'success' | 'warning' | 'info';

export interface LabelSpec {
  text: string;
  tone: Tone;
}

function spec(map: Record<string, LabelSpec>, key: string | null | undefined): LabelSpec {
  if (!key) return { text: '—', tone: 'outline' };
  return map[key] ?? { text: key, tone: 'outline' };
}

// ── 订单（总纲 §5.2 内部状态机）────────────────────────────────────────────
const ORDER_STATUS: Record<string, LabelSpec> = {
  new: { text: '新单', tone: 'info' },
  purchasing: { text: '采购中', tone: 'info' },
  purchased: { text: '已采购', tone: 'info' },
  inbound: { text: '国内在途', tone: 'info' },
  at_relay: { text: '中转点已收', tone: 'warning' },
  handed_over: { text: '已交运', tone: 'success' },
  in_transit: { text: '国际在途', tone: 'success' },
  delivered: { text: '已签收', tone: 'success' },
  completed: { text: '已完成', tone: 'success' },
  cancelled: { text: '已取消', tone: 'destructive' },
  returned: { text: '退货', tone: 'destructive' },
};

export const orderStatus = (v: string | null | undefined): LabelSpec => spec(ORDER_STATUS, v);

export const ORDER_STATUS_OPTIONS = Object.entries(ORDER_STATUS).map(([value, s]) => ({ value, label: s.text }));

// ── 采购任务（总纲 §5.1 状态机）────────────────────────────────────────────
const TASK_STATUS: Record<string, LabelSpec> = {
  pending: { text: '待执行', tone: 'secondary' },
  executing: { text: '执行中', tone: 'info' },
  ordered: { text: '已下单', tone: 'info' },
  paid: { text: '已付款', tone: 'info' },
  shipped: { text: '货源已发', tone: 'warning' },
  closed: { text: '已完结', tone: 'success' },
  exception: { text: '异常', tone: 'destructive' },
};

export const taskStatus = (v: string | null | undefined): LabelSpec => spec(TASK_STATUS, v);

export const TASK_STATUS_OPTIONS = Object.entries(TASK_STATUS).map(([value, s]) => ({ value, label: s.text }));

// ── 执行器 / 渠道 ─────────────────────────────────────────────────────────
const CHANNEL: Record<string, LabelSpec> = {
  self_use: { text: '1688 自用版', tone: 'secondary' },
  cross_border: { text: '1688 跨境版', tone: 'secondary' },
  manual: { text: '人工渠道', tone: 'secondary' },
};

export const channelLabel = (v: string | null | undefined): LabelSpec => spec(CHANNEL, v);

export const EXECUTOR: Record<string, string> = { auto: '自动', manual: '人工' };

// ── 异常（总纲 §5.2 判定清单）─────────────────────────────────────────────
const EXCEPTION_CODE: Record<string, string> = {
  purchase_timeout: '超时未采购',
  purchase_failed: '1688 下单失败',
  address_invalid: '地址校验失败',
  ship_deadline_near: '发货截止临近',
  domestic_stalled: '国内段/中转点停滞',
  ship_failed: '备货失败（ship_failed）',
  arbitration: '平台仲裁',
  unknown_status: '表外状态',
  price_changed: '采购价变动',
  tracking_stalled: '物流轨迹停滞',
};

export const exceptionCode = (v: string | null | undefined): string => v ? EXCEPTION_CODE[v] ?? v : '—';

export const EXCEPTION_CODE_OPTIONS = Object.entries(EXCEPTION_CODE).map(([value, label]) => ({ value, label }));

export const REF_TYPE: Record<string, string> = { order: '订单', purchase_task: '采购任务', shipment: '包裹' };

// ── 店铺 / 凭据 ───────────────────────────────────────────────────────────
const SHOP_MODE: Record<string, string> = { rfbs: 'rFBS', fbp: 'FBP', local: '俄本土' };
export const shopMode = (v: string | null | undefined): string => (v ? SHOP_MODE[v] ?? v : '—');

const SHOP_STATUS: Record<string, LabelSpec> = {
  active: { text: '启用', tone: 'success' },
  paused: { text: '暂停', tone: 'warning' },
};
export const shopStatus = (v: string | null | undefined): LabelSpec => spec(SHOP_STATUS, v);

const CREDENTIAL_KIND: Record<string, string> = {
  ozon_api_key: 'Ozon Api-Key',
  alibaba_app: '1688 应用密钥',
  alibaba_token: '1688 买家 token',
};
export const credentialKind = (v: string | null | undefined): string => (v ? CREDENTIAL_KIND[v] ?? v : '—');

// ── 中转点 / 包裹 ─────────────────────────────────────────────────────────
const RELAY_KIND: Record<string, string> = { forwarder: '货代仓', own_warehouse: '自有仓' };
export const relayKind = (v: string | null | undefined): string => (v ? RELAY_KIND[v] ?? v : '—');

// 物流单号来源与 tpl_integration_type（总纲 §7.4）
export const TRACKING_SOURCE: Record<string, string> = { ozon: 'Ozon 生成', seller: '卖家回传' };

const TPL_INTEGRATION: Record<string, string> = {
  ozon: 'Ozon 自有配送（不传单号）',
  aggregator: '外部承运商·Ozon 登记（不传单号）',
  '3pl_tracking': '外部承运商·卖家登记（需传单号）',
  non_integrated: '卖家自送（需传单号+三段轨迹）',
  hybrid: '俄邮混合（项目未涉及）',
};
export const tplIntegration = (v: string | null | undefined): string => (v ? TPL_INTEGRATION[v] ?? v : '—');

/** 需要我方回传单号的 tpl_integration_type（总纲 §7.4）。 */
export const NEEDS_SELLER_TRACKING = new Set(['3pl_tracking', 'non_integrated']);

// ── 货源 / 映射 ───────────────────────────────────────────────────────────
const PLATFORM: Record<string, string> = { '1688': '1688', pdd: '拼多多', taobao: '淘宝' };
export const platformLabel = (v: string | null | undefined): string => (v ? PLATFORM[v] ?? v : '—');
