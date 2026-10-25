# DeepSeek 网页 Search/Fetch 适配

`pkg/websearch` 提供供应商无关的网页 Search/Fetch 端口，以及 DeepSeek 的内置适配器。它只负责供应商协议、输入边界、Bearer 鉴权、响应解码和可重试传输；调用方仍必须在应用层执行当前身份、授权范围、证据冻结和输出前复核。

## 配置

客户端从环境变量读取密钥，密钥只保留在进程内存中：

```powershell
$env:DEEPSEEK_API_KEY = "<rotated-key>"
```

```go
provider, err := websearch.NewProvider(websearch.Config{
    Provider:  "deepseek",
    APIKeyEnv: "DEEPSEEK_API_KEY",
})
result, err := provider.Search(ctx, websearch.SearchRequest{
    Query:       "memory safety",
    Count:       5,
    NeedContent: true,
})
page, err := provider.Fetch(ctx, websearch.FetchRequest{URL: result.Results[0].URL})
```

## 应用层 HTTP 入口

当 Bootstrap 的 `WebSearch` 注入了 `websearch.Service` 后，以下入口可用：

```text
POST /api/v1/web-search
POST /api/v1/web-fetch
```

两个入口都要求单个 `Authorization: Bearer ...`，并要求 `space_id`。Search 请求体支持 `query`、可选 `count`（默认 10）、`ori_question` 和 `need_content`；Fetch 请求体支持 `url` 和 `space_id`。未注入服务时保持 503，不会隐式启用外部网络访问。

生产调用应为客户端设置较短的上层 deadline，并在业务层限制结果数量和正文长度。客户端对 408、429 和 5xx 做有限重试；不会重试其他 4xx。它不会把外部网页直接当作平台已发布文档，也不会自动绕过平台授权或证据核验。

本地验证：

```powershell
& '.\.tools\go\bin\go.exe' test ./pkg/websearch
```

`Config.Provider` 通过注册表选择实现；DeepSeek 是当前内置实现，其他供应商可通过 `websearch.NewRegistry` 注册，不需要修改应用层接口。Bootstrap 已注册上面的两个 HTTP 路由；未注入服务时返回503，本地个人命令行和网页默认没有装配这个联网服务。网页结果尚未接入默认知识库 Retriever，不会自动作为已发布知识输出。

受控协议、鉴权和HTTP路由测试不等于供应商在线检查；真实检查需要显式设置`DEEPSEEK_LIVE_TEST=1`运行`TestLiveSearchAndFetch`，只发送公开查询和一次结果抓取，禁用重试。官方端点和认证约定见[Search/Fetch 指南](https://api-docs.deepseek.com/guides/search/)。当前整体验收将适配器/协议检查与本地知识库功能分别记录，不把联网适配器当作已经完成的知识库联网召回。
