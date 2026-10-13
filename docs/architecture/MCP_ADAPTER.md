# MCP Server 接入架构 V1

## 1. 选定验证基线

首轮协议基线为2025-11-25，官方Go SDK候选锁定v1.8.0，commit `3f3b699b2b67e1ed033a63d6651671dab53c2d32`。其兼容表包含该协议，go.mod要求Go1.25.0；源码证据和哈希见 [SDK核验清单](evidence/mcp-go-sdk-v1.8.0-source-review.json)、[固定README](https://github.com/modelcontextprotocol/go-sdk/blob/3f3b699b2b67e1ed033a63d6651671dab53c2d32/README.md)。SDK支持范围不代表本产品已测试全部版本。首轮只接受基线版本，更新兼容列表必须另做协议与授权回归。

SDK仅接入传输层，不进入应用服务和领域类型。替换SDK时四项工具、业务DTO、授权和错误语义保持相同。当前未安装为业务依赖或运行服务器。

## 2. 连接和身份

端点设计为 `/mcp`，采用Streamable HTTP。初始化、协议协商、JSON-RPC与传输行为由SDK提供；应用网关校验版本白名单与请求认证。GET不提供独立推送流时按协议返回405，不把REST问答SSE直接挂到MCP GET。协议依据见 [官方传输规范](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)。

V1选择无状态传输模式，业务内容和任务状态不存于MCP会话。每个HTTP请求验证身份并生成Principal。未来采用会话时，会话必须绑定主体/发行方/客户端，仍需每次认证与当前授权；MCP-Session-Id不能作为访问凭据。代理不得用全局用户变量保存Principal。

受控伙伴可使用平台服务Key作为明确的私有集成认证；这不等同已实现通用MCP OAuth互操作。面向通用Agent客户端提供OAuth资源服务模式：受保护资源发现、授权服务器发现、针对MCP资源的令牌签发与校验；优先对接成熟身份服务，不自行实现OAuth密码或授权码算法。[官方授权规范](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)

OAuth令牌验证issuer、签名/内省、有效期、audience和客户端授权；scope只表示可调用动作，具体空间/KB仍由平台授权。用户委托必须能验证用户与客户端的绑定，权限取用户、客户端和请求范围交集。WeKnora调用使用平台内部服务凭据，原客户端令牌不透传，MCP验证后的身份通过平台应用上下文传递。

外部部署使用HTTPS，验证Origin白名单；本地默认仅监听localhost。无Origin的受控非浏览器客户端仍执行认证。反向代理只信任配置的代理跳数，不能从任意Forwarded/Host头推导OAuth资源地址；规范资源URI来自部署配置。

## 3. 工具和schema映射

| 工具 | 输入schema源 | 正常输出 | 业务动作 |
|---|---|---|---|
| knowledge_search | SearchRequest | SearchResult | 允许范围内已发布证据检索 |
| knowledge_read | ReadRequest | ReadResult | 当前/获准历史内容读取 |
| knowledge_ask | AskRequest移除stream字段 | AskResult | 非流式最终回答，反思由平台预算控制 |
| knowledge_feedback | FeedbackRequest | FeedbackResult | 记录意见/建议草稿，不审批发布 |

schema源为 [伙伴OpenAPI](openapi/partner.v1.json)。构建工具定义时将本地引用转成自包含的$defs，不能要求客户端读取仓库路径或外部URL。错误输出使用相同Error对象，outputSchema需允许正常结果或Error；isError与对象类型对应。MCP端禁止stream参数，REST端才选择业务SSE输出。

已生成的自包含目录见 [四工具机器契约](mcp/tools.v1.json)。它是设计产物，不是已运行的tools/list响应。执行 `python docs/architecture/build_mcp_contract.py` 从OpenAPI重建，或加 `--check` 检查文件是否过期；不能手工修改生成目录造成REST/MCP漂移。

结果structuredContent包含平台DTO，content同时放等价JSON文本供兼容客户端使用；两者必须由同一对象序列化，不能分别生成不同答案。readOnlyHint等工具注解只是提示，实际授权由服务端执行。反馈是有副作用的工具，但有平台幂等键；不暴露批准、发布和任意workflow.run工具。[官方工具规范](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)

首版只声明tools，不启用roots/sampling/elicitation或客户端任意工具调用。业务查询反思由平台完成，不请求伙伴客户端扩大授权或执行陌生工具。工具目录可以固定为四项；某主体缺少反馈权限时tools/call仍明确拒绝。未来按主体过滤目录时，目录缓存必须绑定授权上下文。

## 4. 错误、取消与限额

未知工具或无效JSON-RPC消息走协议错误；结构合法但参数/业务失败走isError=true，返回安全Error。身份缺失/令牌失效在HTTP认证层拒绝；OAuth缺少动作scope给出协议要求的challenge。具体KB越权不暗示升级scope就能读到它，按平台资源隐藏规则处理。

无答案是AskResult.status=insufficient_evidence，不是工具异常。超时、撤回或授权变化中断应用执行并返回安全错误，绝不把已有但未重新校验的证据作为成功结果。取消通知和请求context传播到应用预算与适配器；HTTP断连不被视为反馈写入自动回滚，反馈重试依靠幂等记录。

限制消息字节、每主体并发、查询长度、证据数、回答tokens和整体预算。MCP进度通知只说明阶段，不发送未复核证据，不作为问答增量协议。初期不支持长期MCP任务/断线结果重放，调用方需要显式重试，写工具沿用原幂等键。

## 5. 验收清单

| 编号 | 场景 | 要求 |
|---|---|---|
| MCP-01 | 初始化协商、正确/错误协议header、非法Origin | 仅基线通过，错误被安全拒绝；无原生后端旁路 |
| MCP-02 | 两主体并发、会话/Key替换、伪造user_id | 上下文不串用，不把会话当身份，拒绝冒充 |
| MCP-03 | 错误audience、过期/撤销令牌、OAuth发现 | 验证失败不调用业务层；资源/授权服务器发现一致 |
| MCP-04 | 四工具与同主体REST结果对照 | DTO、引用、权限、错误一致，stream不会泄漏到工具输入 |
| MCP-05 | 正常/错误结果的structuredContent与文本 | 输出schema通过，序列化一致，isError正确 |
| MCP-06 | 取消、断连、超时、重复反馈 | 有界执行，合法幂等结果；断连不产生重复草稿 |
| MCP-07 | 切换检索引擎或SDK版本 | 伙伴工具名及业务契约不变，全部权限/发布场景重跑 |

本次为来源核验和设计交付。SDK完整协议符合性、OAuth服务实现、客户端互操作和SDK替换均缺运行证据，不能只用JSON样例通过判定完成MCP接入。

结构检查运行 `python docs/architecture/check_mcp_contract.py`：四工具、八个自包含输入/输出Schema、正常/错误DTO和禁止stream/user_id的样例通过，私有错误字段被拒绝。文本与structuredContent的一致性只验证了设计样例，不证明SDK处理器实现已满足；运行处理器必须独立验证isError/输出对象对应关系和序列化一致性。
