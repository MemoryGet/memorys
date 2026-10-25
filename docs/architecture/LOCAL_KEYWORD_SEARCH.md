# 本地 PostgreSQL 关键词搜索装配

个人本机使用优先参考 [本地命令行](LOCAL_CLI.md)，由显式命令生成身份、权限、配置和样例。
本页保留手动装配的参数与边界；HTTP 服务本身不自动初始化或发布数据。

`memorys-server` 支持显式开启单数据库、单 KB、单关键词通道搜索。
复用 `knowledge.SearchService`、`ConfiguredPlans` 和 PostgreSQL 原生适配器。
服务不会自动发放身份、授权、插入示例业务数据或发布文档。

## 开启方式

在工程根目录的 PowerShell 中配置以下环境变量。数据库密码可通过
`PGPASSWORD` 或 PostgreSQL 的密码文件提供，无须包含在 URL 中。
`.env` 文件不会由程序自动加载。

```powershell
$env:MEMORYS_DATABASE_URL='postgres://postgres@127.0.0.1:5432/memorys?sslmode=disable'
$env:MEMORYS_SEARCH_ENABLED='true'
$env:MEMORYS_OAUTH_ISSUER='https://memorys.local'
$env:MEMORYS_OAUTH_AUDIENCE='memorys-search'
$env:MEMORYS_OAUTH_SUBJECT_TYPE='user'
$env:MEMORYS_OAUTH_JWKS_FILE='E:\ai\memorys\.local\identity\jwks.json'
$env:MEMORYS_PROFILE_FILE='E:\ai\memorys\.local\identity\profile.json'
$env:MEMORYS_OAUTH_TOKEN_MAX_LIFETIME='15m'
& '.\.tools\go\bin\go.exe' run ./cmd/memorys-server
```

上面两个 JSON 路径是示意路径，启动前需要提供实际文件：

- JWKS：只有 RS256 公钥，`kid` 唯一；私钥不交给服务。
- Profile：通过 `configprofile.Seal/Encode` 封存的快照；Index 为 independent，
  Index 和 Query 均只配置一条 keyword 路由。不启用向量、图谱、融合、重排、生成或反思。
- 数据库：已应用 `withdraw_contract_v1`，身份发行方、签名公钥摘要、主体、客户端和
  token 登记状态与 JWKS、JWT claims 一致；只具备签名的 JWT 仍不能访问数据。
- 权限及发布：已封存的当前 ACL、Profile 绑定、审核规则、当前存储/后端代际以及
  通过提交、审核、索引准备和最终发布形成的有效正文及索引。

仅安装 PostgreSQL 或建表不满足这些业务前提。缺少它们会拒绝搜索，不能用 HTTP 200 空结果
掩盖装配失败。未设置开关或设为 `false` 时，保留默认未装配的 503 行为；
设为 `true` 后文件、配置不合法则拒绝启动，启动错误不会输出数据库 URL 或底层错误。

## 请求

```powershell
$headers = @{Authorization = "Bearer $env:MEMORYS_ACCESS_TOKEN"}
$body = @{query='关键词';space_id='space';kb_ids=@('kb');top_k=10} | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8080/api/v1/knowledge-os/search' `
  -Headers $headers -ContentType 'application/json' -Body $body
```

临时令牌由发行方提供，不保存在仓库或报告中。省略 `kb_ids` 时解析主体当前获权的 KB；
本实现实际搜索只支持一个 KB，多 KB 请求会拒绝。显式 `kb_ids: []` 仅执行身份校验并返回空集。
查询 UTF-8 字节上限为 4096。搜索限流为每实例至多 4 个活跃请求；连接池有限时
并发数不超过连接上限的一半，给事务内的当前身份/权限复核预留连接。等待可取消且受请求期限约束。
一个连接池应只绑定一个此类本地宿主，不要在运行时降低连接上限。

## 当前支持与边界

已发布修订正文的 `revision` 文本引用会核对当前批准/发布头、实际索引、冻结正文双摘要和摘录。
本宿主尚未装配独立原件读取 Grant：`original` 文件来源会返回安全的 503；
不会将来源改写为 revision 来跳过验证。草稿、撤回、旧代际或撤销批准不应进入结果。
中文使用当前原生 `simple` 全文分词；尚无专业中文分词或 Recall 质量结论。

令牌、主体、客户端、签名密钥撤销和 ACL 改变在数据库读边界与最终输出核验中重新检查。
这不构成网络发送与撤权的原子屏障，也不代表企业生产部署验收。

`/health` 表示 HTTP 进程存活，`/ready` 只检查当前已装配的数据库依赖，
均不代表已有可检索文档。停止服务时先等待 HTTP 请求退出，再关闭数据库池。

## 可复现的 Windows 实库验证

```powershell
python docs/architecture/check_postgres_windows.py `
  --pg-bin 'D:\Program Files\PostgreSQL\18\bin' `
  --report '.local/architecture-check/local-search-postgres18-windows.json'
```

该命令使用安装目录中的 PG 二进制创建独立临时实例（默认端口 55438），
运行现有 SQL/仓储探针及新增搜索场景，然后停止实例并移除临时文件。
不会连接用户的 5432 数据库。测试身份和签名密钥只属于临时 fixture，
报告始终 `production_acceptance=false`。本地架构回归仍使用 `check_architecture.py`。
