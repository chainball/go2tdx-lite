# go2tdx-lite

Go 语言通达信（TDX）7709 行情协议的 clean-room 实现，一个纯 Go 的行情数据客户端库。

## 特性

- 实时行情、K 线、分钟线、逐笔成交
- 集合竞价、证券代码列表、公司资料与财务数据
- 内置服务器列表与自动测速选优（`DialBest`）
- 服务器端文件读取（如行业字典、日线归档）
- 运行依赖仅 `golang.org/x/text`（GBK 解码），无其他第三方依赖
- 离线测试，协议响应用内置 fixture 校验

## 安装

```bash
go get github.com/chainball/go2tdx-lite
```

## 快速开始

```go
package main

import (
	"fmt"

	"github.com/chainball/go2tdx-lite"
)

func main() {
	// 从内置服务器列表测速，选取最快的 32 台上线
	c, err := go2tdx.DialBest(32)
	if err != nil {
		panic(err)
	}
	defer c.Close()

	// 实时快照
	quotes, _ := c.Quotes().Snapshots([]string{"000001", "600000"})
	fmt.Println(quotes)

	// 日 K 线（最近 10 根）
	bars, _ := c.Bars().Get("000001", go2tdx.Day, 0, 10)
	fmt.Println(bars)
}
```

也可以手动指定服务器：

```go
c, _ := go2tdx.Dial(go2tdx.Server{Name: "s1", Host: "103.221.142.66", Port: 7709})
```

## API

| 模块 | 方法 | 说明 |
|---|---|---|
| `Session` | `Handshake` / `Heartbeat` | 握手、心跳 |
| `Quotes` | `Snapshots` / `Legacy` / `Refresh` / `Category` | 实时行情快照、分类报价 |
| `Bars` | `Get` / `GetIndex` | K 线（个股 / 指数） |
| `Minutes` | `Today` / `History` / `Recent` / `Aux` / `Sparkline` | 分钟线、分时副图 |
| `Trades` | `Today` / `History` | 逐笔成交 |
| `Auctions` | `Series` | 开盘集合竞价 |
| `Codes` | `List` / `Count` | 证券列表、数量 |
| `Corporate` | `CapitalChanges` / `FinanceBatch` | 股本变动、财务数据 |
| `Resources` | `Read` / `ReadFile` | 服务器端文件读取 |

辅助函数：`NormalizeCode`（代码归一化）、`PriceDivisor`（价格除数）。

## K 线周期

`Min1` `Min5` `Min15` `Min30` `Min60` `Day` `Week` `Month` `Quarter` `Year`

## 服务器

- `DefaultServers` — 8 台主站
- `StandardServers` — 92 台候选，`DialBest` 启动时测速选优

## 协议

7709 TCP 行情协议的逐命令字段说明见 [`docs/COMMANDS_7709.md`](docs/COMMANDS_7709.md)。

## 测试

```bash
go test ./...
```

测试离线运行，不发起真实网络请求。

## 免责声明

本项目为通达信 7709 协议的净室实现，仅供学习研究用途，详见 [DISCLAIMER](DISCLAIMER.md)。

## License

[MIT](LICENSE)
