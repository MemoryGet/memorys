# 模块依赖边界与自动检查

本规范落实[分层设计](DESIGN.md#2-分层及依赖规则)。当前自动检查覆盖pkg/与cmd/下所有非测试Go源文件，包含未被当前平台build tags选中的文件；不覆盖尚未建设的internal/业务层或frontend/。检查通过证明静态import边界，不能证明实际数据隔离、远程授权或数据迁移。

## 1. 当前允许的依赖

下列路径省略仓库模块前缀。依赖按完整包路径匹配；不允许用目录前缀自动放行子包。未登记包及新增外部依赖默认拒绝。

| 包 | 允许的仓库内依赖 | 用途 |
|---|---|---|
| contracts / canonical | 无 | 标准端口、领域类型与确定性编码 |
| kernel | contracts | 注册、配置与生命周期；不能导入具体实现 |
| retrievalplan / reflection / queryread | contracts | 编排只使用端口，不识别具体适配器 |
| publicationprojection | contracts、canonical | 标准投影与请求关系校验 |
| modulewire | contracts、canonical | 显式数据协议映射 |
| moduletransport | contracts、modulewire | HTTP与流接收实现 |
| accessgrant | canonical | 隔离签名验证实现 |
| fusion | contracts、kernel | 本地融合实现及其受控工厂 |

fusion导入kernel是实现提供注册工厂的依赖，kernel不得反向导入fusion。retrievalplan不得直接导入fusion；测试可以组合两者验证标准端口。后续应用服务的装配在入口完成，不能为调用具体实现而扩大编排包的依赖。

contracts、canonical和查询/反思/投影编排包禁止net、net/*、database/*和os/exec。所有当前登记包禁止C、plugin和unsafe，符合首版受控编译注册、重启替换的选择。标准库通过实际GOROOT解析识别，不能因import路径没有点就把它当成标准库。

唯一登记的第三方Go依赖是accessgrant中的go-jose/v4；它是签名实现依赖，禁止泄漏到contracts或其他包。SDK版本仍由go.mod/go.sum及构建锁规则固定。未来供应商SDK应登记在独立适配器包，经过许可、类型和依赖审查后扩展规则。

cmd/architecture-*为离线工具，各入口有精确依赖清单；它们不是正式REST/MCP服务器。允许的数据编解码依赖不授予业务应用访问适配器的权限。

## 2. 可重复检查

检查代码：[dependencies_test.go](../../tests/architecture/dependencies_test.go)。

```powershell
.\.tools\go\bin\go.exe test -count=1 -v ./tests/architecture
.\.tools\go\bin\go.exe test -count=1 ./...
.\.tools\go\bin\go.exe vet ./...
```

扫描使用Go AST读取import，别名、匿名import及注释不会绕过规则。测试替身不受生产依赖清单限制，原因是它们需组合多个实现进行契约对照；该豁免不适用于普通Go文件。新增包需要先明确职责和允许依赖，修改清单并提供拒绝场景，不能直接放行整个模块前缀。

## 3. 本次证据与剩余门槛

87个非测试Go源文件通过扫描。三项测试覆盖生产扫描、九种拒绝场景及允许端口/隔离SDK场景。拒绝场景包含供应商类型、SQL/HTTP耦合、编排依赖具体模块、新包未登记、伪标准库路径、匿名导入、加密SDK泄漏和动态插件。

完整Go测试与vet通过后保存[证据](evidence/dependency-boundaries-review.json)。这不是反射、运行时服务定位器、SQL跨模块读表或配置路由的检测器；尚无业务层、Vue、真实后端适配器及两种实现的迁移/回退证据。进入业务实现时须扩展到internal/和前端的依赖门，结合共同契约与真实替换试验验收。整体阶段B仍部分完成。


2026-10-14登记pkg/contentverify：仅依赖contracts/canonical与标准库，作为标准冻结JSON摘要核验纯包，不允许数据库、HTTP、进程调用或供应商SDK。该登记不证明读取提供者或实际授权行为。

2026-10-14登记MVP入口/API/存储/解析/草稿/分块/内存索引与检索包：HTTP在api、文件在storage，业务纯包通过contracts依赖访问；测试允许跨模块组合，正式装配/生产实现仍未验。

2026-10-14登记pkg/adapters受控工厂：仅依赖contracts/kernel/canonical及parser/chunk本地实现，禁止直接HTTP/数据库/文件访问；实物文件操作仍通过BlobStore依赖。

2026-10-14登记pkg/postgres数据库适配（仅contracts与标准库SQL），及cmd/architecture-pg-repositories离线驱动探针；仅后者允许pgx/stdlib，contracts/业务纯包不得依赖驱动。


## 4. 事务仓储的替换边界

Metadata、PublicationHistory、CleanupPlan、Idempotency、Audit、Outbox 和 MigrationJournal 保持独立业务端口，但同一次业务提交必须由同一个 UnitOfWork 提供事务句柄。部署时替换整个事务实现组合，并核对每个必要端口及事务能力；不能将多个独立连接/远程服务的成功响应当作共同原子提交。

仓储实现可在事务组合内部替换或扩展，前提是参与同一底层事务并通过共同故障、幂等及迁移测试。远程审计平台可通过 outbox 消费权威审计副本；该投递和观测平台不能替代事务内的审计记录。若新组合无法提供全部必要端口，启动和相关业务必须拒绝准入。

目前 pkg/postgres 的撤回片段已有历史/计划、CAS、命令预留、事件与撤回专用审计，但缺少完整元数据/审计通用端口、迁移日志、生产身份与部署装配，仍不能登记为完整 metadata_uow 提供者。


2026-10-14登记 pkg/withdrawal：仅依赖 contracts 的撤回应用编排纯包，禁止SQL/HTTP/供应商SDK；离线PG探针在入口组合 withdrawal 与 postgres。标准 WithdrawalUnit 为进程内草案，完整生命周期及服务装配尚未验收。


2026-10-14允许 pkg/withdrawal 依赖 canonical，用于撤回命令语义编码；仍禁止SQL、HTTP及供应商SDK。预留摘要由业务准备器计算，数据库适配器只保存/核对，不定义供应商摘要语义。


2026-10-14登记pkg/cleanup：仅contracts/canonical，禁止SQL/HTTP/供应商SDK；离线PG入口组合存储与单项消费者。适配器解析、执行准入和实际回执核验保留明确独立端口，未提供生产后端。


2026-10-14登记adapters对cleanup/index本地实现的依赖，用于标准CleanupExecutor适配；index.GenerationState只在实现/适配器内部使用，不进入contracts。离线PG入口可组合该实际本地实现，业务Worker仍只依赖contracts/canonical。


2026-10-14登记indexsnapshot纯校验包（contracts/canonical），index及postgres通过该公共规范验证快照。业务端口只使用平台IndexProjection/IndexSnapshot；PG目录绑定与内存暂存恢复留在实现内，不放行SQL到纯包。
