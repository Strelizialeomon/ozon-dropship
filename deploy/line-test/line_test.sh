#!/usr/bin/env bash
# 线路实测 —— 在目标地区（国内 / 香港 / 新加坡）的机器上连续探测 Ozon 与 1688 网关，
# 产出 P50 / P95 延迟与失败率，供 owner 拍板服务器放哪（总纲 §13.2）。
#
# 用法：./line_test.sh [-d 24h] [-i 60] [-o 输出目录] [目标文件]
#   -d 探测时长：24h / 90m / 3600（秒），默认 24h
#   -i 每轮间隔秒，默认 60
#   -o 输出目录，默认 results-<起跑时间戳>
#   目标文件默认本目录 targets.txt（每行：标签<空白>URL[<空白>METHOD]）
#
# 可选鉴权：本目录放 auth.env（0600），写 OZON_CLIENT_ID=... / OZON_API_KEY=...
#   设了就对 api-seller.ozon.ru 的请求附上这两个头（只调只读接口）。
#   不设也能跑——只测网络路径；要验「鉴权链路也通」才需要。
#
# 判定口径：curl 失败（超时 / 连不上 / TLS 错）或 HTTP ≥ 500 = 失败；
#   其余（含未鉴权的 401 / 400）= 可达——服务端答话即证明线路通。
# 只探测、不写数据（Ozon 用 /v1/roles 读密钥信息；1688 用 currentTime 读系统时间）。
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
TIMEOUT=10

DURATION=24h
INTERVAL=60
OUT=""
TARGETS_FILE=""

usage() { sed -n '2,17p' "$0"; exit "${1:-0}"; }

while getopts "d:i:o:h" opt; do
  case "$opt" in
    d) DURATION=$OPTARG ;;
    i) INTERVAL=$OPTARG ;;
    o) OUT=$OPTARG ;;
    h) usage 0 ;;
    *) usage 1 ;;
  esac
done
shift $((OPTIND - 1))
[ $# -gt 0 ] && TARGETS_FILE=$1
[ -n "$TARGETS_FILE" ] || TARGETS_FILE="$SCRIPT_DIR/targets.txt"
[ -r "$TARGETS_FILE" ] || { echo "读不到目标文件：$TARGETS_FILE" >&2; exit 1; }

case "$DURATION" in
  *h) DURATION_S=$(( ${DURATION%h} * 3600 )) ;;
  *m) DURATION_S=$(( ${DURATION%m} * 60 )) ;;
  *s) DURATION_S=${DURATION%s} ;;
  *)  DURATION_S=$DURATION ;;
esac
[ "$DURATION_S" -gt 0 ] || { echo "-d 给了个零时长" >&2; exit 1; }

# 可选鉴权（只在 Ozon 目标上加头）
if [ -r "$SCRIPT_DIR/auth.env" ]; then
  # shellcheck source=/dev/null
  . "$SCRIPT_DIR/auth.env"
fi
OZON_CLIENT_ID=${OZON_CLIENT_ID:-}
OZON_API_KEY=${OZON_API_KEY:-}

# ── 读目标 ────────────────────────────────────────────────
LABELS=(); URLS=(); METHODS=()
while read -r label url method _rest; do
  [ -z "$label" ] && continue
  case "$label" in \#*) continue ;; esac
  LABELS[${#LABELS[@]}]=$label
  URLS[${#URLS[@]}]=$url
  METHODS[${#METHODS[@]}]=${method:-GET}
done < "$TARGETS_FILE"
[ ${#LABELS[@]} -gt 0 ] || { echo "$TARGETS_FILE 里没有有效目标" >&2; exit 1; }

[ -n "$OUT" ] || OUT="results-$(date -u +%Y%m%d-%H%M%S)"
mkdir -p "$OUT"
RAW="$OUT/raw.csv"
echo "ts_utc,target,ok,http_code,rc,dns_s,tcp_s,tls_s,ttfb_s,total_s,err" > "$RAW"

# ── 汇总（也用于中断时收尾）────────────────────────────────
pctl() { # stdin: 排好序的数值列；$1 = 分位
  awk -v p="$1" '{v[NR]=$1} END{
    if (NR==0) { print "NA"; exit }
    i=int(NR*p+0.999999); if (i<1) i=1; if (i>NR) i=NR
    printf "%.1f", v[i]*1000
  }'
}

summarize() {
  local end_ts t n fail rate p50 p95 t50 t95
  end_ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  {
    echo "# 线路实测汇总（${START_TS} ~ ${end_ts}，UTC；时长 ${DURATION_S}s，间隔 ${INTERVAL}s）"
    echo
    echo "| 目标 | 样本 | 失败 | 失败率 | 总延迟 P50/P95 (ms) | 首字节 P50/P95 (ms) |"
    echo "|---|---|---|---|---|---|"
    while read -r t; do
      [ -n "$t" ] || continue
      n=$(awk -F, -v t="$t" 'NR>1 && $2==t' "$RAW" | wc -l | tr -d ' ')
      fail=$(awk -F, -v t="$t" 'NR>1 && $2==t && $3!="ok"' "$RAW" | wc -l | tr -d ' ')
      if [ "$n" -gt 0 ]; then rate=$(awk -v f="$fail" -v n="$n" 'BEGIN{printf "%.1f%%", f*100/n}'); else rate=NA; fi
      p50=$(awk -F, -v t="$t" 'NR>1 && $2==t && $3=="ok" {print $10}' "$RAW" | sort -n | pctl 0.5)
      p95=$(awk -F, -v t="$t" 'NR>1 && $2==t && $3=="ok" {print $10}' "$RAW" | sort -n | pctl 0.95)
      t50=$(awk -F, -v t="$t" 'NR>1 && $2==t && $3=="ok" {print $9}'  "$RAW" | sort -n | pctl 0.5)
      t95=$(awk -F, -v t="$t" 'NR>1 && $2==t && $3=="ok" {print $9}'  "$RAW" | sort -n | pctl 0.95)
      echo "| $t | $n | $fail | $rate | $p50 / $p95 | $t50 / $t95 |"
    done < <(awk -F, 'NR>1 {print $2}' "$RAW" | sort -u)
    echo
    echo "口径：P50 / P95 = 最近秩（nearest-rank）；延迟只统计成功样本；"
    echo "失败 = curl 出错（超时/连不上/TLS）或 HTTP ≥ 500。原始数据见 raw.csv。"
  } > "$OUT/summary.md"
  cat "$OUT/summary.md"
}

# ── 主循环 ────────────────────────────────────────────────
START_TS=$(date -u +%Y-%m-%dT%H:%M:%SZ)
STOP=0
trap 'STOP=1' INT TERM
trap 'summarize' EXIT

echo "开始探测：${#LABELS[@]} 个目标 × 每 ${INTERVAL}s 一轮 × 共 ${DURATION_S}s；输出到 $OUT"
DEADLINE=$(( $(date +%s) + DURATION_S ))
ROUND=0
while [ "$(date +%s)" -lt "$DEADLINE" ] && [ "$STOP" -eq 0 ]; do
  ROUND=$((ROUND + 1))
  ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  i=0
  while [ "$i" -lt "${#LABELS[@]}" ]; do
    label=${LABELS[$i]}; url=${URLS[$i]}; method=${METHODS[$i]}
    hdrs=""
    case "$url" in
      *api-seller.ozon.ru*)
        if [ -n "$OZON_CLIENT_ID" ] && [ -n "$OZON_API_KEY" ]; then
          hdrs="-H Client-Id:$OZON_CLIENT_ID -H Api-Key:$OZON_API_KEY"
        fi ;;
    esac
    errfile="$OUT/.err.$$"
    rc=0
    # shellcheck disable=SC2086
    out=$(curl -sS -o /dev/null --max-time "$TIMEOUT" -X "$method" $hdrs \
      -w '%{time_namelookup} %{time_connect} %{time_appconnect} %{time_starttransfer} %{time_total} %{http_code}' \
      "$url" 2>"$errfile") || rc=$?
    err=$(tr -d ',\n' < "$errfile" 2>/dev/null | cut -c1-120); rm -f "$errfile"
    if [ "$rc" -eq 0 ]; then
      read -r dns tcp tls ttfb total http <<< "$out"
      if [ "$http" -lt 500 ]; then ok=ok; else ok=fail; fi
    else
      dns=NA; tcp=NA; tls=NA; ttfb=NA; total=NA; http=000; ok=fail
    fi
    printf '%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n' \
      "$ts" "$label" "$ok" "$http" "$rc" "$dns" "$tcp" "$tls" "$ttfb" "$total" "$err" >> "$RAW"
    i=$((i + 1))
  done
  printf '\r第 %d 轮完成（%s）' "$ROUND" "$ts"
  sleep "$INTERVAL"
done
echo
echo "探测结束（共 $ROUND 轮）。"
