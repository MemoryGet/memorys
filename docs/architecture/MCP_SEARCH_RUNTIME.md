# MCP 搜索运行接入

更新：2026-10-16。官方 Go SDK v1.8.0 已进入独立 `pkg/mcpgateway` 适配层；应用/领域契约不依赖 SDK 类型。原生实库与独立响应复核已通过，范围限于下述搜索接入。

## 当前范围

采用仓库已选协议 2025-11-25、Stateless Streamable HTTP 和 JSON 响应。HTTP 只支持 POST，GET/DELETE 拒绝；不使用 MCP-Session-Id 作为身份或保存业务内容。每次 HTTP 请求先执行平台认证，业务搜索仍由统一应用服务重新认证、授权及最终来源复核。

当前是受控私有平台 Bearer 接入，不是通用 OAuth 资源服务器。OAuth 受保护资源/授权服务器发现、资源 URI/audience 的完整部署绑定、授权流程和普通客户端互操作仍待完成。外部部署的 TLS、代理与并发配额也不能据本轮本地测试视为验收通过。

默认 tools/list 仅注册knowledge_search；显式配置统一读取应用后同时注册knowledge_read，见[读取增量](PARTNER_READ_DESIGN.md)。ask/feedback仍不注册。协议基线、Origin 白名单、消息大小、总并发与 30 秒预算在网关执行。无 Origin 的受控 Agent 仍必须认证。当前总并发限制不代表已完成每主体配额。

## 契约与数据路径

工具定义从伙伴 OpenAPI 生成，文档目录和适配器嵌入目录由同一生成器写出并检查一致。构造网关时拒绝不匹配的业务目录；协议字段别名、版本绕过、session/task 参数不能改变基线行为。

数据路径：官方 SDK JSON-RPC/HTTP → 请求独有的工具处理器 → 进程内伙伴 DTO 适配 → SearchService → 当前身份/权限/配置/发布/来源检查。进程内复用 PartnerSearchHandler 保持 DTO 和错误语义一致，不发生额外 REST 网络跳转，也不绕过应用认证。后续可抽出独立伙伴编解码器，保持业务契约不变。

客户端令牌只用于当前平台认证，未作为 WeKnora 或其他提供方内部服务凭据透传。工具参数不能覆盖主体；没有全局当前用户变量。身份和 HTTP 请求 context 由各次请求独立持有。

正常和错误结果都使用平台 DTO。structuredContent 和 content 中的 JSON 文本来自同一序列化结果，isError 与业务错误对应。输入/输出使用 SDK 同版本生态的 JSON Schema 实现检查，参数错误不回显原始校验细节或私有提供方错误。

默认服务器注册 /mcp，但未配置网关时返回能力不可用；正式生产依赖装配仍待完成。

## 已验证和仍缺部分

定向测试通过官方 SDK 初始化、单工具目录、搜索双格式结果、错误结果、版本/Origin/伪造主体拒绝、两次模拟身份并发不串用，以及 HTTP 请求取消到达业务服务。已验证的是 HTTP 取消；跨请求 MCP cancellation 通知尚未完成，不能据此关闭全部取消验收。

原生试验使用真实 RS256 与主库当前状态、授权目录、配置、SQL 检索和来源复核，比较同主体 REST/MCP 证据；验证错误协议版本和已撤销令牌在协议分派前拒绝。独立复核须核对实际工具定义、输入/输出 Schema、双格式等价及错误状态。

MCP-01～07 仍为部分证据：OAuth/完整客户端互操作、真实多主体数据隔离矩阵、全部四工具、完整取消/反馈幂等、SDK替换、远程 Grant 和发送/撤权屏障未完成。Recall、性能及迁移回退继续保持开放。

## 依赖记录

SDK固定版本与原先commit核验的 README、go.mod、LICENSE、mcp/server.go 字节摘要一致。Go模块校验与tidy差异检查通过；依赖图为SDK新增若干模块，并将x/sync从v0.17.0提升到v0.20.0，须随本轮全量回归一起复核。这不是全部传递依赖许可或供应链审计。

源码依据：[固定SDK版本](https://github.com/modelcontextprotocol/go-sdk/tree/v1.8.0)、[既有源码核验](evidence/mcp-go-sdk-v1.8.0-source-review.json)。

## 本轮验收记录

509项源码输入，PG15/16/18每版235项原生检查、Go全量测试与静态检查、Python契约与OpenAPI检查通过；真实文件转换和解析器替换检查通过。MCP独立检查覆盖实际目录/输入输出Schema、结构化与文本等价、REST证据一致性，三版临时实例端口和目录独立确认清理。见[MCP证据](evidence/mcp-search-runtime-review.json)及[统一证据](evidence/unified-local-checks.json)。本记录不关闭完整MCP或生产验收。

## 官方客户端互操作补验

新增 TestOfficialClientStreamableSearch，使用同版本官方SDK的StreamableClientTransport与ClientSession，经真实localhost HTTP进行初始化、initialized通知、工具列表与搜索调用；客户端收到的文本JSON与结构化结果相等。定向包测试、架构依赖检查及go vet通过。应用仍是测试fixture，本证据不代替实库链路、不证明其他客户端或OAuth互操作。当前明确关闭可选独立SSE通道，协议固定2025-11-25。

本次只修改适配器测试文件；上一节509项输入/235检查为补验前归档快照，不能称为包含此新测试的全量运行结果。

## 当前读取签名分页增量

显式注入CurrentReadService并配置分页后，knowledge_read接收原有ReadRequest.cursor并返回ReadResult.next_cursor；无需修改工具Schema，REST和MCP共用相同签名模块和逐页认证/来源验证。527项输入在PG16共243项原生检查通过，实际REST游标在MCP继续、两页完整/无重复、Schema与双格式、篡改和真实令牌撤销拒绝通过独立复核。签名期间撤权另有应用故障测试，签名完成后才执行最终控制检查。见[读取证据与边界](PARTNER_READ_DESIGN.md)。原件授权仍为fixture，历史、生产密钥/依赖装配、OAuth、其他工具及发送屏障未完成；本轮未重验PG15/18，不关闭完整MCP或产品验收。

2026-10-17已新增[已发布历史版本](HISTORICAL_READ_RUNTIME.md)分支：显式revision_id在应用中申请独立history权限，REST游标可继续MCP读取同一历史快照，不开放主体/授权覆盖参数，也不增加伪实现工具。539项输入在PG16通过250项原生检查，实际旧索引删除后历史原文、双格式及批准未发布/撤销拒绝通过独立复核。原件/治理授权仍为fixture，完整历史生命周期与生产验收保持开放；上一段527/243为历史快照。
