// 格式化工具：时间 / 金额 / 剩余天数。
// 后端时间一律 UTC（RFC3339），界面按需显示北京 / 莫斯科时间（总纲 §13.1）。

const TZ = {
  bj: 'Asia/Shanghai',
  msk: 'Europe/Moscow',
} as const;

export type TimeZoneKey = keyof typeof TZ;

const dtfCache = new Map<string, Intl.DateTimeFormat>();

function formatter(tz: TimeZoneKey, withTime: boolean): Intl.DateTimeFormat {
  const key = `${tz}-${withTime}`;
  let f = dtfCache.get(key);
  if (!f) {
    f = new Intl.DateTimeFormat('zh-CN', {
      timeZone: TZ[tz],
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      ...(withTime ? { hour: '2-digit', minute: '2-digit', hour12: false } : {}),
    });
    dtfCache.set(key, f);
  }
  return f;
}

/** 时间戳/ISO 字符串 → 「2026/10/07」或「2026/10/07 14:30」；空值显示 '—'。 */
export function formatDateTime(iso: string | number | null | undefined, tz: TimeZoneKey = 'bj'): string {
  if (iso === null || iso === undefined || iso === '') return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return formatter(tz, true).format(d);
}

export function formatDate(iso: string | number | null | undefined, tz: TimeZoneKey = 'bj'): string {
  if (iso === null || iso === undefined || iso === '') return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return formatter(tz, false).format(d);
}

/** 金额 + 币种（后端 DECIMAL 以字符串或数字给到前端都可能，统一两位小数）。 */
export function formatMoney(amount: number | string | null | undefined, currency?: string | null): string {
  if (amount === null || amount === undefined || amount === '') return '—';
  const n = typeof amount === 'string' ? Number(amount) : amount;
  if (Number.isNaN(n)) return '—';
  const s = n.toFixed(2);
  return currency ? `${s} ${currency}` : s;
}

/** 距到期还有几天（向上取整，负 = 已过期）；无到期时间返回 null。 */
export function daysUntil(iso: string | null | undefined): number | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  return Math.ceil((d.getTime() - Date.now()) / 86_400_000);
}
