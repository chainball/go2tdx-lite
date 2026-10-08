# 7709 TCP 协议全量字节参考（clean-room 实现源）

> 来源：eltdx Rust `crates/eltdx-protocol/src/{frame.rs,request.rs,response.rs,unit.rs,limits.rs,commands/*.rs}` + Python `src/eltdx/protocol/*` + 冻结 fixtures `tests/fixtures/7709/**`。
> 所有整数 little-endian。价格 wire 为 milli(×1000)。文本名称 GBK、代码 ASCII 数字。日期 yyyymmdd u32 LE。

## 1. 传输与帧

- **请求帧** 12B 头：`0C` + msg_id u32 LE + control u8(01) + length u16 LE ×2 + msg_type u16 LE + data。`length = len(data)+2`。
- **响应帧** 16B 头：`B1 CB 74 00` + control u8 + msg_id u32 LE + reserved u8 + msg_type u16 LE + zip_length u16 LE + length u16 LE + payload。
  - `zip_length == length` → payload 明文；否则 `zip_length` 字节为 zlib(zlib 头+DEFLATE)，解压到 `length`。
  - 字节流按前缀 `B1 CB 74 00` 重同步（跳过垃圾）。
- 冻结请求例（frame.rs 测试，msg_id=123, type=0x044E, data=`00 00 a7 26 35 01`）：`0c 7b 00 00 00 01 08 00 08 00 4e 04 00 00 a7 26 35 01`。

## 2. 二进制原语

- **TDX signed varint**：首字节 bit0-5 量级、bit6 符号、bit7 续；后续字节 bit0-6 量级（shift 6/13/20…）、bit7 续。例 `0x01→1`、`0x41→−1`、`0x81 0x01→65`。
- **decode_k（行情价格）**：5 varint = current(绝对 milli) + 4 delta（相对 current）：`last_close=current+Δ1, open=current+Δ2, high=current+Δ3, low=current+Δ4`。
- **get_volume（成交量/额 u32）**：`s=i32(v); logpoint=s>>24; hleax=(s>>16)&0xFF; lheax=(s>>8)&0xFF; lleax=s&0xFF; base=2^(logpoint*2−0x7F); high = hleax>0x80 ? base*(64+(hleax&0x7F))/64 : base*hleax/128; scale = hleax&0x80 ? 2:1; middle=base*lheax/32768*scale; low=base*lleax/8388608*scale; 结果=base+high+middle+low`。K线 `volume_lots=volume/100`。
- **price_divisor**：ETF 前缀 15/16/50/51/52/53/56/58 → 10；否则 1；被 0x044D decimal 覆盖（0..2→1，3→10，4→100，≥5→10^(d−2)）。
- **kline 时间解码**（4B 域）：period.raw 0/1/2/3/7/8 → 打包日期 u16 LE（`year=(x>>11)+2004, month=(x%2048)/100, day=(x%2048)%100`）+ 分钟 u16 LE；13 → u32 LE 秒（2003-12-31 起）；其余(4/5/6/9/10/11) → u32 LE yyyymmdd，时间 15:00。
- **market 映射**：sz=0/sh=1/bj=2。`normalize_code`：8 字符前 2 市场 + 后 6 数字；6 数字首字符 `92→bj`、`6/9→sh`、`0/1/2/3→sz`、`8→bj`。
- **minute_index_label**：index<120 → total=571+index（09:31 起）；else 661+index（13:01 起）。

## 3. 命令字节布局

### session

**handshake (0x000D)** — 请求 data = 1 字节 `0x01`。响应 ≥189B：
`0` unused u8 | `1` year u16 | `3` day u8 | `4` month u8 | `5` minute u8 | `6` hour u8 | `7` unused u8 | `8` second u8 | `9..25` 8×u16 session_minutes_1 | `25..41` 8×u16 session_minutes_2 | `42..46` server_date_1 u32 | `46..50` unknown_time_1 u32 | `50..54` server_date_2 u32 | `54..58` unknown_time_2 u32 | `58..68` flags_raw 10B | `68..152` server_name GBK NUL-pad 84B | `152..160` tail_control 8B | `160..189` product_tag GBK NUL-pad 29B。

**heartbeat (0x0004)** — 请求 data 空。响应 ≥10B：`0..6` reserved 6B | `6..10` server_date u32 yyyymmdd。

### codes

**security_count (0x044E)** — 请求 6B：market u16 | client_date u32。响应 2B u16 count。

**security_list (0x044D)** — 请求 14B：market u16 | start u32 | limit u32 | 4×0。响应 2B count u16 + N×37B：
code 6B ASCII | multiple u16（手数）| name 16B GBK NUL-pad | volume_ratio_base f32 | decimal u8 | previous_close f32 | unknown3 4B。
fixture 实证：`normal` 请求 `0c 05 00 00 30 01 10 00 10 00 4d 04 | 00 00 00 00 00 00 40 06 00 00 00 00 00 00`；记录 `303030303031 6400 | c6bd b0b2 d2f8 d0d0 | ... 02 | 0ad72f41 | ...` → code=000001, multiple=100, name=平安银行, decimal=2, preclose f32=10.99。`bj_empty` 请求 market=`02 00`。

### limits

**special_limits (0x0452)** — 请求 14B：start_index u16 | 12×0。响应 2B count u16 + N×13B：market_id u8 | code_num u32（`{:06}`）| upper_price f32 | lower_price f32。

### bars

**klines (0x052D)** — 请求 42B：market u16 | code 6B | period.raw u16 | period.parameter u16 | start u16 | count u16（1..800）| adjust u16（0 none/1 qfq/2 hfq/3 fixed_qfq/4 fixed_hfq）| anchor_date u32（0）| 20×0。
响应 2B count u16 + N 变长记录：
time 4B（见 §2 kline 时间解码）| 4 signed varint（open_delta, close_delta, high_delta, low_delta）| volume u32 | amount u32 | [仅 index] up_count u16 + down_count u16。
重建：`open = last_close + open_delta; close = open + close_delta; high = open + high_delta; low = open + low_delta; last_close = close`（初始 0，全 milli）。stock 最小记录 16B、index 20B。
周期 raw：0=5m,1=15m,2=30m,3=60m,4=day,5=week,6=month,7=1m,8=自定义分,9=自定义日,10=quarter,11=year,13=秒。

### corporate

**capital_changes (0x000F)** — 请求 2B count u16 + N×7B（market_id u8 + code 6B），max 200。响应 2B block_count u16 + 块循环：9B 头（market_id u8 | code 6B | record_count u16）+ N×29B：
market_id u8 | code 6B | reserved_7 u8 | date u32 yyyymmdd | category u8（1除权除息 2送配股上市 3非流通股上市 4未知股本变动 5股本变化 6增发新股 7股份回购 8增发新股上市 9转配股上市 10可转债上市 11扩缩股 12非流通股缩股 13送认购权证 14送认沽权证 15重整调整）| c1 f32 | c2 f32 | c3 f32 | c4 f32。
单位：股本类(2,3,5,7,8,9,10) c 值 ×10000；category 6 仅 c3 ×10000；其余原值。

**finance_batch (0x0010)** — 请求同 capital_changes。响应 2B count u16 + N×143B：market_id u8 | code 6B | finance_info 136B。
finance_info：`0..4` liu_tong_gu_ben f32 | `4..6` province u16 | `6..8` industry u16 | `8..12` updated_date u32 | `12..16` ipo_date u32 | `16..136` 30×f32：zong_gu_ben, guo_jia_gu, fa_qi_ren_fa_ren_gu, fa_ren_gu, b_gu, h_gu, eps, zong_zi_chan, liu_dong_zi_chan, gu_ding_zi_chan, wu_xing_zi_chan, gu_dong_ren_shu, liu_dong_fu_zhai, chang_qi_fu_zhai, zi_ben_gong_ji_jin, jing_zi_chan, zhu_ying_shou_ru, zhu_ying_li_run, ying_shou_zhang_kuan, ying_ye_li_run, tou_zi_shou_yu, jing_ying_xian_jin_liu, zong_xian_jin_liu, cun_huo, li_run_zong_he, shui_hou_li_run, jing_li_run, wei_fen_li_run, mei_gu_jing_zi_chan, bao_liu_2。

### minutes

**today_intraday (0x0537)** — 请求 12B：market u16 | code 6B | reserved_tail 4B（`00 00 00 93`）。响应 2B count u16 + 2B reserved_zero u16 + N 记录（3 varint：price 首条绝对后续相对、avg、volume）。

**historical_intraday (0x0FB4)** — 请求 11B：trading_date u32 | market u8 | code 6B。响应 2B count u16 + 4B prev_close f32 + N 记录（3 varint：price_delta 累积、aux_delta、volume）。

**recent_intraday (0x0FEB)** — 请求 11B：date_selector u32（`0xFED62304 − python_date_ordinal(date)`）| market u8 | code 6B。响应 2B count u16 + 4B prev_close f32 + 4B open_price f32 + N 记录（3 varint，首条为基准）。

**intraday_aux (0x051B)** — 请求 28B：market u16 | code 6B | 19×0 | kind u8（0x00 buy_sell_strength / 0x0B volume_comparison）。响应 2B count u16 + N：kind=0x00 → (series_a varint + series_b varint)；kind=0x0B → 8B（previous_day f32 + current_day f32）。

**sparkline (0x0FD1)** — 请求 37B：market u8 | 0 | code 6B | 16×0 | selector u8（1）| 0 | window u16（20）| fixed u32（`0x01000000`）| 5×0。响应 ≥42B：market_id u8 | reserved | code 6B | 16 reserved | selector_echo u8 | reserved | reserved_param u32 | reserved 4B | max_count u16 | base_price f32 | price_count u16 | price_count×f32。

### trades

**today_ticks (0x0FC5)** — 请求 12B：market u8 | 0 | code 6B | start u16 | count u16（1..1800）。响应 2B count u16 + N：time_minutes u16 | 5 varint（price_delta 累积/100、volume、order_count、status、tail）。status：0买 1卖 2中性 8竞价快照 5盘后定价。

**historical_ticks (0x0FC6)** — 请求 16B：trading_date u32 | market u16 | code 6B | start u16 | count u16。响应 2B count u16 + 4B price_base f32 + N 记录（同 today_ticks 形状，末 varint = reserved_zero）。

### auctions

**auction_series (0x056A)** — 请求 28B：market u8 | 0 | code 6B | trading_date u32（0=今日）| mode u32（3）| 4×0 | start u32 | limit u32（500）。响应 2B count u16 + N×16B：minute_of_day u16 | price f32 | matched_volume u32 | unmatched_signed i32 | reserved 0x0E | second u8。

### quotes

**snapshots (0x054C) / legacy_quotes (0x053E)** — 请求同形：8B 固定前缀 `05 00 00 00 00 00 00 00` | count u16 | N×7B（market u8 + code 6B）。legacy 需 ≥1。
snapshots 响应：2B reserved u16 | 2B count u16 | N 变长记录（按 7B market+code marker 切分）：market u8 | code 6B | active1 u16 | decode_k 5 价格 varint | time varint | unknown_after_time varint | total_hand varint | current_hand varint | amount u32 | inside_dish varint | outer_disc varint | unknown_after_outer varint | open_amount varint | bid_delta 价格 varint | ask_delta 价格 varint | bid_volume varint | ask_volume varint | tail_raw。
legacy_quotes 响应：2B reserved | 2B count | N 变长：market u8 | code 6B | active1 u16 | close(绝对) varint | pre_close_diff | open_diff | high_diff | low_diff | server_time | unknown_after_time | total_hand | current_hand | amount u32 | inside_dish | outer_disc | unknown_after_outer | open_amount | **5×**（bid_delta varint, ask_delta varint, bid_volume varint, ask_volume varint）| trading_status u16 | 4×tail varint | 可选 4B 尾（rise_speed i16 + active2 u16，末条或下条 marker 不紧跟时出现）。

**refresh_stream (0x0547)** — 请求 2B count u16 + N×11B（market u8 + code 6B + cursor u32），max 100。响应**整体 XOR 0x93**：解码后 2B count u16 + N 变长（marker 切分）：market u8 | code 6B | active u16 | decode_k 5 varint | update_time u32 | status varint | total_hand | current_hand | amount u32 | inside_dish | outer_disc | unknown_after_outer | open_amount | 5×（buy_delta 价格 varint, sell_delta 价格 varint, buy_volume varint, sell_volume varint）| tail_raw。空 payload `93 93` → `00 00`。

**category_quotes (0x054B)** — 请求 18B：category u16（6=沪深A股）| sort_type u16（0x0000代码 0x0006现价 0x000A成交额 0x000E涨幅 0x001C封单额 0x001D开盘金额 0x002E涨速 0x00CC短换手 0x00D0量涨速 0x010A开盘抢筹 0x010C 2分钟金额 0x0119开盘涨幅 0x011A最高涨幅 0x011B最低涨幅 0x011E回撤 0x011F攻击）| start u16 | count u16 | sort_reverse u16 | 5 u16 | filter u16 | 1 u16 | 0 u16。响应 2B header u16 | 2B count u16 | N 变长：market u8 | code 6B | active1 u16 | close(绝对) + pre_close_diff + open_diff + high_diff + low_diff varint | server_time | neg_price | total_hand | current_hand | amount u32 | inside_dish | outer_disc | after_outer | open_amount | bid1_diff | ask1_diff | bid_vol1 | ask_vol1 | **56B 定长尾**（status_or_sort u16 | rise_speed i16 | short_turnover i16 | min2_amount f32 | opening_rush i16 | extra_pair 10B | vol_rise_speed f32 | depth f32 | extra_meta 24B | active2 u16）。

### resources

**file_content (0x06B9)** — 请求 308B：offset u32 | size u32（1..60000）| path ASCII NUL-pad 300B。响应 4B chunk_len u32 + content；`chunk_len < size` → 末块。

### money_flow（专用 transport）

**money_flow (0x0FFC)** — 请求 38B：market u8 | 0 | code 6B | 30×0。wire msg_id = `(msg_id&0xFF) | (0x2D<<8) | (0x7E<<16)`（route 0x7E bits16-23、channel 0x2D bits8-15）。冻结帧：`0c 5e 2d 7e 00 01 28 00 28 00 fc 0f` + 38B。
响应：块循环直到 payload 耗尽。块头 40B：market_id u8 | reserved | code 6B | 30 reserved | reported_count u16。N×88B（reported_count=0 → 剩余全读）：date u32 | 21×u32 raw[0..21]。
解码：`total_amount = f32(raw[1])`；`buckets[0..16]` 由 raw[10..18] 每 u32 拆两 u16（`buckets[2i]=lo16, buckets[2i+1]=hi16`）。派生：`scale=total_amount/50000`；`main_net=(b0−b1+b4−b5)/50000*total_amount`；`main_ratio=(b0−b1+b4−b5)/500`；`main_buy_net=(b2−b3+b6−b7+b10−b11+b14−b15)/50000*total_amount`；super_large=b0−b1、large=b4−b5、medium=b8−b9、small=b12−b13（各 ×scale）；main_buy 对应 b2−b3/b6−b7/b10−b11/b14−b15。

## 4. 服务器列表

- 主站（8 台，硬编码于 go2tdx/servers.go）：`103.221.142.66/65/69/80`、`103.251.85.200`、`115.238.90.165`、`110.41.147.114`、`116.211.121.102`，全 `:7709`。
- 资金流专用（35 台，同 servers.go）：全 `:7709`。
