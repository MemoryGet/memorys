2026-10-18最新增量：删除控制outbox使用identity-backed `event_seq`和固定20位游标，避免随机请求ID keyset在并发写入时跳过事件；当前626项源码快照的本地/PG16证据均通过。

2026-10-18最新增量：生产Runtime新增可选`ProductionArtifactAssembler`路径，按同一代JWKS/Profile/模块目录快照调用宿主，制品加载失败即拒绝启动；基础assembler接口保持兼容。

2026-10-18当前稳定窗口更新：最新625项源码快照（含FactoryRegistry和Profile→kernel工厂解析）通过PG16.15 WSL完整探针，SQL/Go仓储检查与资源清理完成；fixture身份/部署仍使`production_acceptance=false`。见[当前PG证据](evidence/current-postgres16-wsl.json)。

2026-10-18实库证据：PG16 WSL探针最近稳定窗口通过326项检查且资源已清理；随后为覆盖游标单调约束的复跑捕获约4.83秒墙钟跳变并拒绝，当前完整PG验收保持未通过，production_acceptance仍为false。

2026-10-18当前稳定窗口：同一源码快照的PG16.15 WSL完整探针再次通过，SQL/Go仓储检查和临时资源清理完成；fixture身份/部署仍使`production_acceptance=false`。见[当前PG证据](evidence/current-postgres16-wsl.json)。

2026-10-18最新增量：Bootstrap Runtime新增Ready探针，Runner未启动、已停止或最近投递失败时报告不可用；无后台Runner的HTTP组合保持可用。

2026-10-18最新增量：Runner隔离告警钩子panic，告警后端故障不会终止事件消费主循环，也不会改变游标提交和重放边界。

2026-10-18最新增量：Runner新增只读Snapshot，可供Bootstrap健康检查采集游标、启动/停止状态和最近错误，但不被当作业务完成或租约有效性的证明。

2026-10-18最新增量：事件Runner新增可注入OnError告警钩子，投递失败会保留游标并重试，告警接入不改变事件幂等和游标提交边界。

2026-10-18最新增量：PG CursorStore改为单调游标upsert，旧实例覆盖较新游标会失败，避免同一consumer并发/重启时消费位置倒退。

2026-10-18最新增量：Bootstrap新增Runtime生命周期门面，可同时启动/停止注入的事件Runner；退出时等待Runner收敛，HTTP组合与后台消费不再由调用方分别管理。

2026-10-18最新增量：新增`postgres.DeletionControlEventBus`，把删除控制outbox按keyset分页映射为只读EventBus，Runner可直接消费事务事件；发布和任务状态修改仍不允许通过事件总线绕过应用层。

2026-10-18最新增量：HTTP组合根已抽出为`pkg/bootstrap`，集中校验搜索/读取/删除/profile等能力绑定，部分装配失败即拒绝启动，缺失能力继续fail-closed返回503。

2026-10-18最新增量：Runner现在要求注入CursorStore，并仅在游标持久化成功后推进本地位置，宿主重启可恢复消费边界；调度、告警和处理器生命周期仍由Bootstrap负责。

2026-10-18最新增量：事件Runner加入可停止周期调度、每轮deadline和游标边界，正常停止不误报取消，宿主仍负责持久游标和处理器装配。

2026-10-18最新增量：[Outbox事件消费运行时](architecture/EVENT_CONSUMER_RUNTIME.md)新增有界、可重放且回执去重的通用消费者，取消事件可由宿主Bootstrap异步投递；失败不推进游标，具体调度和处理器仍由宿主装配。

# AI Knowledge OS V1 架构设计验收报告

2026-10-18最新增量：[删除清理安全取消控制](architecture/DELETION_CONTROL_RUNTIME.md)新增独立control端口、持久cancelled状态及事务审计/outbox；取消要求当前控制授权、任务未终态且无活跃目标租约，unknown/failed/completed不能被重置，SQL进度门禁保持计数和版本约束。本地契约/PG编译检查通过，管理HTTP取消路径已接入应用层，审计装配和PG时钟实库验收仍开放。

2026-10-18最新本地证据：当前读取装配、幂等时钟监控和健康门禁已完成；最新源码清单609项，Go全量/go vet、232项Python及三份OpenAPI通过。PG16当前实库轮次已通过，历史墙钟跳变诊断仍保留，未把历史PG通过快照升级为当前验收。

2026-10-18补充：[运行时墙钟健康门禁](architecture/CLOCK_HEALTH_RUNTIME.md)将墙钟/单调观测做成可复用组件，ConfiguredRuntime要求ClockHealth通过才构造；本地组件、运行时和依赖测试通过。它不改变租约时间或修正系统时钟，生产Bootstrap与PG时钟稳定性仍开放。

2026-10-18最新增量：[当前授权驱动的可替换搜索规划](CONFIGURED_SEARCH_RUNTIME.md)已加入配置Profile、当前事实/发布清单、父摘要及命名模块解析；本地单元/依赖检查通过，尚未把启动服务标为生产装配。

2026-10-18当前验证状态：[治理时钟诊断](GOVERNANCE_CLOCK_DIAGNOSTICS.md)。新增有界审批/首次续租检查点及墙钟/单调观测，实际捕获约4.18秒步变；审批提交年龄为负、续租过期的Go/PG值与当前头匹配，业务守卫保持拒绝。正常完整轮次又观测约5.22秒跳变，故当前PG完整验证未通过，不能用此前586项/326检查快照替代。当前590项输入的Go全量/go vet、232项Python及三份OpenAPI通过；诊断禁止伪装完整通过或覆盖标准报告，失败保留源码摘要，五个私有PG实例及端口清理已核对。未修改业务时间守卫、未延长租约、未调整系统时钟；生产时钟健康、审批/续租稳定性及完整V1仍未验收。

2026-10-18最新增量：[上传读取deadline与取消分类修复](UPLOAD_DEADLINE_RUNTIME.md)。真实HTTP固定读超时先于context定时回调，修复前3/3复现i/o timeout被ctx.Err覆盖成Canceled；新增读取边界分类，仅在实际网络timeout且有效deadline已到时返回DeadlineExceeded。期限内主动取消仍Canceled，Abort错误仍安全ErrCleanup，失败无receipt。六场景各10次共60/60通过，586项输入下Go全量/go vet、228项Python及三份OpenAPI通过；PG16保持326项检查和八组独立实库复核，PG资源清理确认。审批/续租间歇失败及完整V1仍开放；Windows测试空文件已清理，剩余临时目录结构删除被自动审批拒绝，已记录。

2026-10-18最新增量：[清理模块保留绑定与可信装配](CLEANUP_REGISTRY_RUNTIME.md)。新增标准模块来源、PG封存部署目录及按命名实例解析的注册器，核对旧Profile/目标/代际、实际Provider/实现/契约/特性及kernel配置摘要；逐次重查，撤销、错误实现/配置/Profile和已关闭引用均拒绝。真实service授权下，另一任务通过正确绑定完成实际PG索引fence/零行，负向目标unknown隔离且记录保留；SQL封存/单调head约束及独立摘要/目录/租约/效果复核通过。585项输入在PG16通过326项检查、八组独立复核、Go全量/go vet、228项Python和三份OpenAPI，资源清理已确认。目录发布/来源治理仍fixture；生产部署控制、数据集/制品证明、远程Grant/原子写屏障、对账/Purge及完整V1仍开放。重跑遇到既有续租、审批和上传超时间歇失败，已保留并补充诊断，未认定稳定性问题已修复；本轮仅PG16。

2026-10-18最新增量：[清理worker真实身份与独立授权](WORKER_AUTHORITY_RUNTIME.md)。新增标准认证/授权控制器及PG当前冻结目标准入，实际RS256 service身份、token/client/key状态及封存ACL反复核查，任务执行与index.delete分别获权；旧策略、改名作用域、跨KB、完成目标、关闭及发现期间撤权均拒绝。只有清理权限的任务failed/unknown且无实际索引删除；独立索引权限齐备后，另一任务自动完成真实fence/零行，失败任务不重试。579项输入在PG16通过317项检查、七组独立复核、Go全量/go vet、228项Python与三份OpenAPI，资源清理已确认。新源/治理仍fixture，生产身份发放/worker装配、模块注册、远程Grant、对账/并发恢复、其他后端/Purge和完整V1仍未验收；本轮仅PG16。

2026-10-18最新增量：[删除队列发现与有界轮询](DELETION_QUEUE_RUNTIME.md)。新增标准队列端口、PG作用域发现及可取消轮询器；每周期续权，内部keyset游标跨周期保持公平位置，策略变更重置，整页及结果配置核对，终态与有效目标租约任务排除。实库三待处理Job自动完成、实际索引fence/零行、失败unknown无重试、旧策略游标/跨KB/过期/撤权拒绝、取消及其他KB不变通过独立复核。574项输入在PG16通过309项检查，六组独立队列/清理/任务/删除/历史/当前复核、Go全量/go vet、228项Python与三份OpenAPI通过，资源清理已确认。新增任务来源及worker/index授权仍fixture；可嵌入运行循环不等于生产持久调度，生产IAM/装配、并发恢复/对账、其他后端、Purge和完整V1仍开放。初次SQL参数类型失败已保留，本轮仅PG16。

2026-10-17最新增量：[历史正文读取](HISTORICAL_READ_RUNTIME.md)在539项输入下通过PG16共250项原生检查，真实read/history双授权、同文档换版、旧索引删除后历史来源、REST/MCP分页及批准未发布/撤销批准等拒绝结果通过独立复核。当前读取回归、全量Go/go vet、228项Python和三份OpenAPI通过；提供方改变验证基准map的回归已先复现再修复。历史能力不放宽当前读层或搜索动作；完整历史生命周期、生产IAM/原件Grant/发送屏障、默认装配、结构来源、效果及迁移仍未验收。维持架构有条件通过、完整V1未通过；本轮仅PG16。

2026-10-16最新增量：[按文档读取与签名分页](PARTNER_READ_DESIGN.md)在527项输入下通过PG16共243项原生检查，实际两页REST/MCP完整性、Schema/双格式、原件文本、篡改及令牌撤销拒绝通过独立复核；Go全量/go vet、228项Python与三份OpenAPI通过。签名模块位于最终授权前，覆盖签名期间撤权/撤回。测试宿主新增独立20秒查询/输出阶段，未改变业务30秒或任务租约，不能当产品性能证据。仍仅当前版本与粗定位文本，原件Grant为fixture；历史/复杂来源/生产装配/发送屏障/效果和迁移未验收。此前间歇失败有保留记录，不能称稳定性已达标。维持架构有条件通过、完整V1未通过；本轮未重验PG15/18。

2026-10-16最新增量：[当前读取来源验证与REST/MCP](PARTNER_READ_DESIGN.md)在521项输入下通过PG16共240项原生检查与独立原件/Schema/双格式/REST一致性复核，全量Go/go vet、228项Python及三份OpenAPI通过。必需可替换验证器在最终授权前检查整块来源，首个实现限非空粗定位文本；复杂来源、历史/游标、生产原件Grant/发送屏障和正式装配未验收。两次较早阶段间歇失败有归档，之后完整通过不关闭稳定性问题。PG15/18未在本轮重验。维持架构有条件通过、完整V1未通过。

2026-10-16增量：MCP搜索适配已通过受控私有Bearer、本地真实后端及独立Schema/REST一致性复核；当前509项输入在三版PG各235项检查通过。[证据](evidence/mcp-search-runtime-review.json)。维持架构有条件通过、完整V1未通过；OAuth、其他工具、生产装配、效果和迁移仍待验。

2026-10-17追加[历史读取跨主体游标隔离](HISTORICAL_READ_RUNTIME.md)：539项输入在PG16通过252项检查及独立身份/双授权/相同正文复核，第二位真实阅读者成功读取后复用第一位游标被明确拒绝；当前读取回归、Go全量/go vet、228项Python及三份OpenAPI通过，资源清理已独立核对。原件Grant仍fixture，撤回后重发/删除及完整历史生命周期仍待验，既有租约间歇失败不因最终通过而关闭。维持架构有条件通过、完整V1未通过；本轮未重验PG15/18。见[独立证据](evidence/history-subject-runtime-review.json)。

2026-10-17追加[历史来源撤回/重发](HISTORICAL_READ_RUNTIME.md)：540项输入在PG16通过255项检查，实际八条历史及撤回回执证明正常换版、明确撤回、其他版本发布后仍拒绝和显式重新发布目标后的恢复。冻结字节/批准及新封存读取ACL独立复核、当前读取回归、Go全量/go vet、228项Python和三份OpenAPI通过，资源清理已核对。仅来源端口与治理fixture，删除、完整应用游标/发送屏障及全V1仍未验收；既有租约间歇失败保持开放，本轮未重验PG15/18。见[独立证据](evidence/history-lifecycle-runtime-review.json)。

2026-10-17追加[历史读取应用生命周期](HISTORICAL_READ_RUNTIME.md)：540项输入在PG16通过258项检查，真实认证应用在撤回/重发前后两页的完整正文及顺序独立复核；明确撤回及其他版本发布后拒绝，显式重发后新分页通过，旧游标仍明确拒绝。当前读取回归、Go全量/go vet、228项Python及三份OpenAPI通过，临时资源清理已核对。治理/原件权限仍fixture，删除、生命周期REST/MCP传输、跨issuer/客户端及完整V1未验收，既有租约间歇失败保持开放。见[独立证据](evidence/history-app-lifecycle-runtime-review.json)。

2026-10-18追加[历史读取REST/MCP生命周期](HISTORICAL_READ_RUNTIME.md)：541项输入在PG16通过261项检查，实际协议前后两组REST至MCP分页及三类拒绝样本独立复核；重发后旧游标REST 409/conflict与MCP isError，错误无正文/引用/游标，新请求可继续两页。Go全量/go vet、228项Python、三份OpenAPI及当前读取回归通过，资源清理已核对。治理/原件授权仍fixture，删除、跨issuer/客户端、生产Grant/发送屏障及完整V1未验收；既有租约间歇失败保持开放，本轮未重验PG15/18。见[独立证据](evidence/history-transport-lifecycle-runtime-review.json)。

2026-10-18追加[实际文档软删除](DOCUMENT_DELETION.md)：547项输入在PG16通过275项检查，内部服务/实库原子删除及三状态、回滚/冷恢复、真实关联记录/固定清理目标、原件保留和REST/MCP拒绝通过独立复核。Go全量/go vet、228项Python和三份OpenAPI通过，资源清理已核对。治理权限仍fixture，管理DELETE/202投影、生产权限与发送屏障、Purge、并发/完整故障矩阵及完整V1仍未验收；旧任务租约间歇失败保持开放，本轮未重验PG15/18。见[独立证据](evidence/document-deletion-runtime-review.json)。

2026-10-18追加[管理DELETE与202任务](MANAGEMENT_DELETE_RUNTIME.md)：553项输入在PG16通过283项检查，真实HTTP删除专用权限、读取权限拒绝、Job/回执/202映射、重放/两类冲突及令牌/权限撤销独立复核通过；Go全量/go vet、228项Python、三份OpenAPI与历史/当前读取回归通过，资源清理已核对。默认服务仍需显式装配，生产Bootstrap/IAM、任务查询/消费、Purge、提交写屏障与完整V1未验收；既有租约间歇失败继续开放，本轮未重验PG15/18。见[独立证据](evidence/management-delete-runtime-review.json)。

2026-10-18追加[管理任务查询](JOB_STATUS_RUNTIME.md)：561项输入在PG16通过291项检查，实际删除Job的公开Schema/资源/回执及独立任务查看权限复核通过；跨KB/缺失/删除专用主体隐藏和读取期间令牌/权限撤销均无状态泄露。Go全量/go vet、228项Python、三份OpenAPI及删除/历史/当前回归通过，资源清理已核对。仅删除任务初始queued状态，其他任务来源、运行进度/消费、完整TaskRepository、生产Grant/发送屏障与完整V1未验收；既有租约间歇失败继续开放，本轮未重验PG15/18。见[独立证据](evidence/job-status-runtime-review.json)。

2026-10-18追加[删除任务实际消费及进度](DELETION_CLEANUP_RUNTIME.md)：568项输入在PG16通过302项检查，7个真实退役目标的租约/回执摘要、实际删除fence与零残留、GET运行状态、完成重放/空任务/unknown隔离、原件及其他来源保留通过独立复核。伪造任务成功与裸completed更新未绕过后端事实门禁；Go全量/go vet、228项Python、三份OpenAPI及既有回归通过，资源清理已核对。worker及故障源授权仍fixture，持续调度、更多后端、对账/取消重试、Purge、完整TaskRepository与完整V1未验收；既有租约间歇失败仍开放，新增fixture失败有记录。

验收日期：2026-10-12。依据为 [原始方案](../AI_Knowledge_OS_开发计划与模块化架构.md)、现有设计文件、机器契约、Go源码和本次重新执行的检查。用户要求本轮验收架构设计，未将真实组件与产品验收从总目标中删除。

## 1. 结论与适用范围

**总体架构评审有条件通过；V1契约冻结及原方案的完整架构验收暂未通过。**

2026-10-16追加[伙伴搜索POST映射](PARTNER_SEARCH_RUNTIME.md)：505项输入在三版PG各229项检查通过，实际契约路径HTTP请求/成功及认证错误响应按Schema复核，并比对来源/配置。覆盖省略KB当前授权、显式空列表不进入计划与目录失效状态拒绝；全量Go/go vet、228项Python及三份OpenAPI通过。当前仅搜索映射与受控宿主联调，生产依赖、全值域/错误矩阵、其余API/MCP/UI及效果验收仍开放。

2026-10-16补[重排候选预算](SEARCH_RERANKING.md)：502项输入在三版PG各224项检查，实际召回两条且从原第二名返回Top 1，独立核对索引、引用和原件来源。全量Go/go vet、228项Python及三份OpenAPI通过。成熟模型质量、固定Recall、性能和正式产品验收仍开放。

2026-10-16补[替换融合器交付边界](SEARCH_MULTI_ROUTE.md)：502项输入在三版PG各223项检查通过，独立确认Fuser调用前1次、完成后累计2次实际后端检查及原件阶段预算收紧。缺失/拒绝前置复核、非法原始候选和输入别名隔离的单元测试通过；全量Go/go vet、228项Python和三份OpenAPI通过。远程融合服务、Grant/发送屏障、成熟模型效果与正式产品验收仍开放。

2026-10-16追加[多路由搜索与融合](SEARCH_MULTI_ROUTE.md)：502项输入在三版PG各220项检查通过，独立复算RRF分数、贡献来源与配置摘要；路由失败不返回部分结果。Go全量/go vet、228项Python与三份OpenAPI通过。两条实库路由复用同一PG实现；成熟混合检索、完整多库目标规划、降级状态及正式产品验收仍开放。

2026-10-16追加[搜索重排前后复核](SEARCH_RERANKING.md)：500项输入在三版PG各217项检查通过，独立验证排序、来源保留及重排配置摘要；覆盖伪造摘录和重排期间撤销凭据的最终拒绝。全量Go/go vet、228项Python与三份OpenAPI通过。测试重排器不是成熟模型；多路由、反思、正式MCP/UI、生产启动及效果验收仍开放。

2026-10-16追加[统一搜索应用与本地HTTP](KNOWLEDGE_SEARCH_APPLICATION.md)：498项输入在三版PG各214项检查通过；应用调用真实认证/授权/配置、SQL检索、最终来源复核，HTTP响应与实际结果/原件独立关联通过。全量Go/go vet、228项Python和三份OpenAPI通过。当前单路由、测试宿主计划与准入，默认服务器未完成生产装配；正式伙伴API/MCP、全产品及召回性能门槛仍开放。

2026-10-16追加[查询配置装配](PROFILE_MODULE_ASSEMBLY.md)：493项输入在三版PG各211项检查及独立配置/实例/路由/结果绑定通过；具名实例版本/特性检查、同能力多路由、提供方替换与托管关闭单元测试通过。证据范围为查询阶段和受控注入的PG工厂；正式启动、制品真实性、索引阶段、迁移及产品效果仍待验。

2026-10-16追加[配置快照内容与摘要](PROFILE_SNAPSHOT_INTEGRITY.md)：489项输入在三版PG各208项检查及独立Python三层摘要复算通过，当前事实源核验完整平台配置内容。完整模式语义、制品真实性、编译/装配、管理接口及产品效果门槛仍开放。

2026-10-16追加[数据库当前事实源](POSTGRES_CURRENT_FACTS.md)：487项输入在三版PG各205项原生检查及独立关系验证通过。主链路改用当前配置引用和审核/授权头，切换配置后旧请求拒绝。仅验证已登记引用及状态一致性，完整配置目录、摘要真实性、编译/装配与产品集成门槛仍开放。

2026-10-16追加[持久化授权](PERSISTENT_AUTHORIZATION.md)：485项输入在三版PG各198项原生检查通过，独立验证授权请求、Scope和数据库授权行的绑定。当前证据主链路已使用真实签名认证及封存ACL执行，撤权后拒绝。测试发行方/策略登记仍由探针提供；生产管理、CurrentFacts配置源、产品集成和完整验收保持开放。

2026-10-16追加[访问令牌认证](IDENTITY_ACCESS_TOKENS.md)验证：483项输入在PG15/16/18各通过192项原生检查，Go全量测试/go vet、228项Python及三份OpenAPI检查通过。真实RS256认证与主库状态检查接入当前证据复核，但发行方为临时测试密钥、授权与部分配置仍是fixture；不关闭生产身份授权、产品集成、召回和迁移验收。见[本轮证据](evidence/identity-access-token-review.json)。

2026-10-13追加[SSE传输子集](SSE_TRANSPORT.md)验收：8项测试含3项真实localhost net/http场景，验证帧/预算、有限deadline/关闭、模拟逐批拒绝，以及EOF前不向sink发布done；Go全包测试/go vet及87项Python回归通过，见[传输证据](evidence/sse-transport-review.json)。这是接收器与本地网络证据，非真实权限/Grant、模型、完整模块HTTP适配器或后台取消认证，D-03及总验收保持未通过。

2026-10-13复核：补齐[control状态/取消](CONTROL_WIRE_MAPPING.md)及复合search实际Go数据对照。22项控制样本覆盖15类结果、六种状态/两类取消响应并拒绝7类非法变异；Go全包测试/go vet、87项Python回归及8个映射脚本通过，见[执行证据](evidence/control-wire-review.json)。[17操作库存](REMOTE_MAPPING_COVERAGE.md)记录全部操作的局部数据证据；不证明全量值域/领域验证、授权/账本、HTTP/SSE或真实执行，D-03与完整验收仍未通过。

D-03取得[生成/五类事件及顺序规则](GENERATION_WIRE_MAPPING.md)局部证据：三种完整/不足/失败样本、独立Schema前后核对、7项预算/引用/终态/Trace变异拒绝，以及Go包测试/go vet、87项Python回归通过，见[生成映射复核](evidence/generation-wire-review.json)。这是事件data与离线顺序检查，没有模型、SSE/网络、当前授权或实时计量/取消证据；control映射和真实协议门槛仍待完成，D-03未关闭。

D-03已取得[RemoteError安全投影](ERROR_WIRE_MAPPING.md)局部证据：Go对应全部15种Schema错误码，内外关联检查、原始文案/cause隔离与重试建议边界通过，见[错误映射复核](evidence/error-wire-review.json)。Go全包测试/go vet、87项Python回归与独立Schema对照通过；供应商分类、当前授权、HTTP/账本/重试执行和生成/控制网络语义仍待验证，D-03未关闭。

D-03取得[Blob metadata及二进制校验](BLOB_WIRE_MAPPING.md)局部证据：put/open/delete实际Go往返及独立Schema检查，整数size超过2^53无损；Go全包测试/go vet和87项Python回归通过，见[Blob wire复核](evidence/blob-wire-review.json)。文件流EOF/长度/哈希与响应头已做局部检查，但未实现HTTP/multipart、阻塞取消、授权/Grant和存储调用；生成、控制及安全错误映射和真实协议门槛仍未闭合。

D-04补齐[原件复制清单与映射边界](BLOB_COPY.md)：11项专项本地字节/库存检查、87项Python回归和三份Schema一致性检查通过，见[文件复制复核](evidence/blob-copy-review.json)。真实临时文件证明局部长度/摘要与流式读取；没有目标存储适配器、实际复制、持久化映射切换或回退证据。D-04及完整运行验收继续未通过。

D-04后续已补[KB后端迁移状态契约](MIGRATION_STATE.md)：13项专项状态规则测试、76项Python回归和三份Schema生成一致性通过，见[状态契约复核](evidence/migration-state-review.json)。范围仅为同一元数据库内KB后端替换的离线规则；事实来源、租约接管/CAS、当前权限、真实切换/重启，以及数据库/文件迁移状态和typed载荷仍待完成。D-04与完整验收保持未通过。

D-04新增[迁移日志与提交水位](JOURNAL_PROTOCOL.md)及完整事务日志包机器契约。11项专项测试、63项Python回归和日志/导出Schema生成一致性通过，见[日志契约复核](evidence/journal-contract-review.json)。证据范围为合成前驱链、独立期望边界、完整计数/版本/载荷关系，不验证数据库提交顺序、可信Head或原子重放。迁移状态及typed记录/真实运行门槛仍待完成，D-04继续开放。

D-04继续补齐[元数据导出清单](EXPORT_CONTRACT.md)及分段/记录包络校验。9项专项测试、52项Python回归、Schema生成一致性和部署结构检查通过，见[导出清单复核](evidence/export-manifest-review.json)。该证据不验证各类payload完整字段、真实权威快照、附件字节、认证凭据、提交水位或导入事务，D-04尚未关闭。

D-04后续补齐[受控构建锁](BUILD_LOCK.md)及离线摘要核对，10项专项测试、43项Python回归和部署结构检查通过，见[构建锁复核](evidence/build-lock-review.json)。覆盖合成制品字节一致性、精确实现覆盖和版本关系；未验证来源信任、实际运行制品、数据库epoch或真实迁移。导出包与迁移水位机器契约仍待完成，D-04未关闭。

D-04已新增[保留与回收契约](RETENTION_CONTRACT.md)、保留策略Schema及离线关系校验，7项专项测试通过；BuildLock、ExportManifest、迁移水位和正式部署对照仍待完成，D-04未关闭。最新证据见[保留契约复核](evidence/retention-contract-review.json)：33项Python测试和部署结构回归通过，未执行真实回收、迁移或数据库事务。

模块化单体、平台权威数据、端口/适配器、受控装配、不可变版本、平台发布指针、作用域前置过滤、维护窗口迁移及回退日志的组合是自洽的，可以继续做契约收敛与验证。没有发现必须推倒总体方案的证据。

初次验收发现3项阻碍契约冻结的缺口。同日复核已补齐D-01的复合端口、内核能力映射和离线整组规则，以及D-02的精确编码、格式字段、两级哈希与跨语言金标，见第7–8节；远程模块机器契约仍未闭合。正式部署/迁移契约及组件选型证据也有待补齐。不能把这一结论表述为“所有模块已可替换”或“产品可上线”。

第9节已补17项远程操作的机器契约及传输规则；D-03仍待完整字段映射/往返对照和真实协议验证，暂未关闭。

本次13项自动检查全部通过，输出见 [执行证据](evidence/architecture-acceptance-checks.json)。其范围是设计结构、生成一致性、假设模块与现有纯校验代码；权限、真实后端过滤、发布事务、迁移、召回和性能没有运行证据。

## 2. 关键设计验收

| 项目 | 权威依据 | 设计判定 | 证据边界 |
|---|---|---|---|
| 稳定上层与替换边界 | [详细架构](DESIGN.md)第1–3、9节 | 通过 | UI/REST/MCP依赖应用服务，平台端口不引入厂商对象；未运行客户端替换试验 |
| 原件、内容与引用归属 | [数据模型](DATA_MODEL.md)、[内容格式](CONTENT_FORMAT.md) | 通过 | 平台保存编辑后标准Block，公开citation与后端ID分离；哈希编码冻结见问题D-02 |
| 审核、发布与撤回 | [一致性协议](CONSISTENCY.md)第2–3节 | 通过 | 候选准备后CAS切换、授权/审批复核、撤回拦截、在途字节边界明确；尚无数据库故障试验 |
| 身份与空间/KB权限 | 详细架构第6节、一致性协议第3节 | 通过 | 机器主体与用户委托区分、范围求交、前后置检查、当前状态闸门；尚无生产实现 |
| 检索、融合与反思 | 详细架构第7节、[模块协议](MODULE_PROTOCOL.md) | 通过 | 规格匹配、排名融合、全局预算、最多两额外轮/30秒；KB扇出与真实效果待验 |
| 任务与外部写入一致性 | 一致性协议第4节、数据模型事务矩阵 | 通过 | 权威租约/fencing、attempt隔离、outbox、消费去重；不能由接口声明证明事务语义 |
| 数据迁移与回退 | 一致性协议第5–6节 | 通过原则设计 | 暂停写入、屏障水位、新命名空间、墓碑、新写入回放；导出包/锁文件机器契约见D-04 |
| 注册、配置与生命周期 | 模块协议、[内核设计](KERNEL_RUNTIME.md) | 部分通过 | 假设模块内核与文件级检查通过；正式归一化、复合映射、制品/持久状态核对未闭合 |
| 伙伴REST与MCP | [标准契约](CONTRACTS.md)、[MCP设计](MCP_ADAPTER.md) | 通过接口设计 | 4项业务工具同源Schema、权限传递与错误规则；无SDK/客户端互操作证据 |
| 成熟组件复用 | [选型决策](DECISIONS.md)、[WeKnora适配](WEKNORA_ADAPTER.md) | 条件通过 | 固定源码证据与复合边界已说明；许可/维护/真实能力矩阵仍不完整 |
| 质量与快速响应 | [验证方案](VALIDATION.md)第4–5节 | 通过评测设计 | Recall@10定义、封存集、越权为0、延迟/成本口径明确；未实测达到90%或性能预算 |

以上“通过”表示设计论证和边界足够明确，不表示实现或最终运行门槛已经通过。

## 3. 20个模块与五类前期成果

原始20个模块均在 [需求覆盖表](REQUIREMENTS_AUDIT.md) 中有设计依据及对应缺失证据。逐项查阅后未发现模块被删去；图谱、记忆保持可选后期能力，不把暂不上线等同取消目标。

用户界面以OpenAPI替换；身份与权限按主体/策略接口替换；接入、文件、解析、分块、嵌入、索引、检索、融合、重排、生成按平台端口替换；审核保留领域状态机；反思只输出建议；记忆/图谱保留作用域与来源；仓储、任务、事件、观测各有数据及事务边界；REST/MCP保持同一应用服务。复合提供方只能按声明的整组替换，不能宣称内部细粒度替换已完成。

| 前期成果 | 本轮验收 |
|---|---|
| 组件能力矩阵 | 已形成候选/门槛/替换成本表；固定版本许可、维护与运行能力核验不完整，尚未完成原要求的逐项核对 |
| 契约设计 | 领域/端口、Schema、4项伙伴与26项管理OpenAPI已成文；冻结前须关闭D-01至D-04 |
| 架构决策记录 | ADR-001至006覆盖底座、数据归属、发布、替换、代际与质量；通过设计交付 |
| 验证方案 | 解析、检索、模型、文件分别有两种候选的替换试验，故障/权限/幂等场景明确；试验设计通过，执行未完成 |
| 伙伴接口 | 检索、读取、带引用问答、反馈四能力及REST/MCP映射齐全；设计通过，运行联调未完成 |

## 4. 问题与关闭标准

下表保留初次验收的发现；后续进展以第7节及之后的复核为准，不将历史运行检查用于证明新文件。

P1表示阻碍相关V1契约冻结，P2表示进入对应实现/组件认证前必须收敛。编号可直接用于后续设计任务，不以测试全部绿色自动关闭。

| 编号/级别 | 发现与影响 | 必须补齐的设计及验证 |
|---|---|---|
| D-01 / P1 | 部署Schema与离线检查允许composite_ingest_search；Go Capability及内核portChecks没有此项，亦无对应复合端口。离线通过的首选复合路线不能原样进入内核 | 确定复合端口的prepare/verify/search及映射，或明确归一化成同实例IndexWriter/Retriever并保留整组约束；冻结唯一方案，加入完整复合配置正例和拆分拒绝测试 |
| D-02 / P1 | 数据模型仅规定UTF-8、稳定顺序/空值等原则，未定义唯一编码。不同语言可能产生不同content_hash，从而使审批与迁移校验不一致 | 定义规范化版本、精确字段/排序、数值/转义/缺省/空值规则及content_hash与blocks_hash的关系；提供中文、emoji、换行、合并表格、媒体和人工修改的金标字节/哈希向量 |
| D-03 / P1 | 远程模块只有/modules/v1路由和服务身份约定；没有完整请求/响应Schema、流式与取消映射、错误/重试及兼容用例 | 至少冻结Parser、Blob/读取能力、IndexWriter/Retriever与模型的远程映射；说明AccessScope验证、幂等/未知提交结果、分页/流、限额与取消；用同组进程内/远程契约样例验证一致 |
| D-04 / P2 | BuildLock仅有字段表，导出/导入包和保留配置也只定义原则；无法据此自动检查制品、完整导出或回退保留窗口 | 增加锁文件、ExportManifest、迁移状态/水位及保留配置的机器契约，明确源码/制品/配置摘要关联；定义归一化器与持久化epoch对照及拒绝路径 |
| D-05 / P2 | 组件矩阵中多数候选尚未锁定版本/许可/依赖；解析保真、中文全文及WeKnora过滤规模未验证 | 为首轮生产候选逐项保存固定版本证据与能力限制，执行两实现对照试验；不满足门槛时按已定独立路线替换，不放宽审核/权限 |
| D-06 / P2 | 旧需求审查仍写“仅有早期骨架、运行装配缺失”，与当前内核/离线检查证据不一致 | 本轮已纠正为“内部内核通过，正式部署和真实组件未验”；继续以检查文件哈希和明确范围记录状态 |

D-01是契约映射缺口，不意味着应拆开WeKnora未证明可拆的内部能力。D-03是模块远程化契约缺口，不应将REST伙伴DTO直接复用为带内部凭据/事务句柄的远程请求。

## 5. 本轮执行证据

全部13项命令重新执行，退出码均为0：

- Go全量包测试（关闭测试结果缓存）与go vet；contracts语句覆盖率92.6%，kernel为89.6%。
- 模块清单、部署结构、装配语义三项校验。
- 伙伴、管理、MCP三项契约检查。
- 管理OpenAPI和MCP目录两项生成一致性检查。
- 文件级配置8项测试，以及完整假设部署文件检查。
- 锁定版本OpenAPI规范校验器验证两份规范，覆盖4项伙伴和26项管理操作。

具体命令、输出、时间和源文件指纹见执行证据。审查还检查了架构分层、端口类型、数据模型、MCP/WeKnora边界及原始条件。自动检查不包括Mermaid实际渲染、Go竞态检查、真实服务或厂商功能验证。

## 6. 原方案最终通过条件

| 原始条件 | 本轮结论 |
|---|---|
| 替换底层后界面/伙伴正常调用兼容 | 未验：没有运行客户端与真实替换前后结果 |
| 同一权限/发布/引用/故障/幂等测试通过 | 未验：目前只有局部关系与假设内核测试 |
| 数据导出、迁移、验证、回退 | 未验：没有含切换后写入/墓碑的演练 |
| 重建后可追溯原件、版本、位置 | 设计明确，未验真实索引重建与引用对照 |
| 能力不足、契约或配置错误明确暴露 | 局部通过：结构、离线语义及内核失败路径；正式启动/后端核验未通过 |
| 同一评测集对比召回、准确性、延迟与成本 | 未验：封存测试集、生产负载及对照报告缺失 |

因此本轮允许继续完善契约和小范围架构验证，不宣告完整目标完成。先关闭D-01至D-03，再冻结涉及的模块/内容/远程契约；D-04在迁移与正式部署实现前关闭，D-05在生产组件认证前关闭。上述运行条件必须按原范围验收，不以文档审核替代。

## 7. D-01复核：设计映射已补齐

选定单一CompositeEngine端口，Go能力与内核类型检查已接入；复合准备不要求后端支持外部Vectors。配置约束补齐复合通道写入/召回同实例、重复排名拒绝，同时允许独立扩展通道。见 [复合契约](COMPOSITE_CONTRACT.md) 与 [复核证据](evidence/composite-contract-review.json)。

Go现有测试及静态检查通过；Python共13项文件/复合测试通过，其中5项直接覆盖复合路线。D-01的设计/假设装配缺口得到解决，但正式部署归一化、运行Profile绑定、真实提供方和迁移仍未通过，完整验收结论保持不变。

## 8. D-02复核：精确编码与哈希设计已补齐

[冻结规范](FROZEN_CONTENT.md)定义AKOS-CJSON/1和akos.freeze/1：精确字段、顺序、转义、Unicode、十进制数值、缺省/空值、域分离前缀及blocks_hash/content_hash关系。Frozen Schema从标准内容生成，Revision补HashFormat；管理响应补格式、块哈希和附件清单。

7项金标保存字节与哈希，Go/Python两实现一致；6项Python测试补充错误输入和哈希差异检查。新证据见 [冻结复核](evidence/frozen-content-review.json)。D-02的设计缺口已收敛，但Go辅助函数不替代Schema、来源/媒体/权限验证，实际提交映射、事务和大载荷流式验证仍待完成。D-03及完整运行验收仍未通过。

## 9. D-03进展：远程机器契约与传输规则

[远程协议](REMOTE_MODULES.md)与modules.v1 OpenAPI覆盖17项解析/文件/索引/检索/模型/状态取消操作，采用类型化包络，明确服务身份与访问Grant、当前授权闸门、二进制/SSE终态、结果未知及attempt隔离。7项离线测试及独立OpenAPI规范检查通过，证据见 [远程协议检查](evidence/remote-contract-review.json)。

本次还发现普通FormatChecker在当前Python环境未验证date-time，已补UTC模式、真实日历验证和有限数值检查，非法日期的拒绝有测试证据。这不代表运行服务已经执行验证。完整字段对照/进程内远程互证、验签、持久调用账本、取消和网络行为仍待完成，D-03未关闭。

## 10. D-03解析部分：实际类型映射与往返证据

[解析映射](PARSER_WIRE_MAPPING.md)已覆盖ParseRequest/ParseResult及各类内容/来源字段，7组金标经真实Go类型解码/编码后与原wire规范字节一致，转换前后均由独立Schema检查。实现拒绝大整数有损转换、内容联合冲突、未知字段、非法UTF-8和不匹配响应；时间进一步收敛为无冗余尾零的UTC纳秒表示。

复核记录见 [解析映射证据](evidence/parser-wire-review.json)。该证据只覆盖DTO映射，不覆盖其他远程端口、完整Schema执行、真正解析、Grant、账本或网络，D-03完整验收未关闭。

## 11. D-03检索链路：规格、过滤与来源字段

[嵌入/检索/重排映射](RETRIEVAL_WIRE_MAPPING.md)完成实际Go/wire对照；初版Generation缺失的state/backend_binding/readiness已补入内部Schema，避免静默丢字段。三个操作的请求/响应在独立Schema与实际Go类型之间往返一致，错模型/维度、空或越界过滤、候选代际越界等有拒绝证据，见 [检索映射复核](evidence/retrieval-wire-review.json)。

这不证明检索后端前置过滤、可信Scope、真实准备结果或召回率。索引/文件/生成/控制映射与生产安全/网络仍未验，D-03及总目标保持未完成。

## 12. D-03索引部分：准备、验证与有界删除

[索引映射](INDEX_WIRE_MAPPING.md)覆盖6项独立/复合操作的实际Go字段往返及独立Schema；对集合/attempt/通道/计数/Spec/私有映射使用独立权威期望核验，删除显式区分generation/chunks。复核记录见 [索引映射证据](evidence/index-wire-review.json)。

DTO检查不等同真实准备证明，fencing传输也不证明当前租约。真实Verify/过滤、映射提交、发布CAS、删除/恢复/回退未执行。文件/生成/控制映射及授权/网络仍待验，D-03保持未关闭。

## D-03追加复核：multipart隔离暂存（2026-10-13）

[公共接收器](BLOB_MULTIPART_TRANSPORT.md)新增7项Go场景，包含真实HTTP客户端中断及客户端保持连接时的服务器deadline。失败无receipt，暂存关闭/删除和清理失败安全错误有证据。见[复核记录](evidence/blob-multipart-review.json)。当前访问回调为模拟实现；完整认证/Grant、存储提交、孤立资源恢复及调用账本仍未验，D-03及总体运行验收不因此关闭。

## 上传确认设计复核（2026-10-13）

[确认协议](BLOB_CONFIRMATION.md)补齐接收后到权威关联的边界，列出9项并发、崩溃、授权、事务及双存储验收要求。未提供确认实现或运行结果，D-03/D-04保持开放。下一设计交付为UploadOperation机器记录和暂存/持久对象桥接，不能只通过接收测试宣称确认成立。

## UploadOperation机器记录复核（2026-10-13）

[记录Schema与离线核对](BLOB_CONFIRMATION.md#6-机器记录与离线核对)拒绝候选/attempt错配、过早结果及不完整未知提交，共7项合成测试。记录声明不是权威观察；报告固定ready_for_confirmation=false。见[复核证据](evidence/upload-operation-review.json)。持久状态转换、存储桥接及UPLOAD-01至09运行门槛未通过，D-03/D-04继续开放。

## 上传状态转换追加复核（2026-10-13）

[fencing高水位与转换规则](BLOB_CONFIRMATION.md#7-状态转换与fencing高水位)新增9项合成测试，弥补恢复清空attempt后令牌历史丢失的设计缺口。真实CAS、守卫观察来源、不可变对象及崩溃恢复未验证。见[复核记录](evidence/upload-transition-review.json)，D-03/D-04及UPLOAD-01至09仍未关闭。

## D-05候选组件追加复核（2026-10-13）

[WeKnora准入核验](COMPONENT_ADMISSION.md)补充固定许可证及第三方边界、默认ParaDB镜像、基础服务依赖、构建开关与profile，官方活动/发布元数据已记录。没有OCI身份、完整依赖许可、安全修复支持或运行替换证据，D-05保持开放；不能将主项目MIT当整套镜像的许可结论。

## 解析部署包追加复核（2026-10-13）

[固定解析依赖与部署边界](PARSER_DEPLOYMENT.md)补齐Python声明/锁及Office/Java/浏览器边界，明确OCR/VLM不由DocReader镜像自动证明。没有构建/启动、真实解析或双实现试验，6项PARSER-DEPLOY门槛仍待执行，D-03/D-05未关闭。

## Qdrant候选追加复核（2026-10-13）

[独立向量适配器设计](QDRANT_ADAPTER.md)固定核对v1.16.2许可及OpenAPI，明确获准组合过滤、候选隔离、标准内容重建和完整流水线对照。Q-01至06未执行，无真实后端过滤/迁移/召回或维护支持证据，D-05及运行替换验收仍开放。

## 召回评分契约追加复核（2026-10-13）

[证据召回评测](RETRIEVAL_EVALUATION.md)提供局部区间覆盖、失败分母及泄露核对，7项合成测试不代表真实后端或90%目标证据。封存集、机器Schema、输出映射、独立标注/质量/性能仍待验，阶段F保持未通过。

## 评测机器Schema追加复核（2026-10-13）

[两份闭集Schema与请求绑定](RETRIEVAL_EVALUATION.md#5-机器schema及请求绑定)新增5项检查，问题/主体/权限/发布快照一致性已纳入评分输入。全量115项Python回归通过；无法从回显声明证明实际调用，封存集来源、真实后端/映射/统计与90%目标仍未通过。见[复核记录](evidence/retrieval-schema-review.json)。

## 评测封存投影包追加复核（2026-10-13）

[投影包摘要与范围校验](RETRIEVAL_EVALUATION.md#6-封存投影包完整性)使用真实临时文件核验UTF-8字节、码点边界、库存和运行绑定，共8项测试。没有原始资料/映射资产封存、可信快照、独立标注或真实后端证据，阶段F及Recall目标保持未通过。见[复核记录](evidence/retrieval-seal-review.json)。

## Grant内部绑定追加复核（2026-10-13）

[内部Bindings契约](ACCESS_GRANT.md)补齐调用身份/动作/摘要和有效期关系，共5项合成检查；不验证签名、凭据来源或当前授权，报告authorized固定false。真实SDK/轮换、Go摘要、身份/访问闸门、账本仍未验，D-03保持开放。

## Grant摘要金标追加复核（2026-10-13）

[6组跨语言摘要](ACCESS_GRANT.md#5-gopython摘要金标)通过Python参考和Go编码对照，含实际ParseRequest解码及完整Scope；Go/Python各2项测试。没有全面17操作或真实token/服务身份/权限/账本证据，D-03保持开放。见[复核记录](evidence/grant-digest-review.json)。

## 全操作Grant摘要追加复核（2026-10-13）

[17操作局部金标](ACCESS_GRANT.md#6-全17操作局部摘要对照)逐项通过独立Schema及实际Go请求Decode，字节/摘要一致、Scope提取一致、非法input变异拒绝。Go一项17子场景与Python两项检查；真实operation动作授权、验签和当前权限仍缺，D-03未关闭。见[复核记录](evidence/operation-grant-review.json)。

## 操作动作策略追加复核（2026-10-13）

[17操作动作与子能力](REMOTE_OPERATION_POLICY.md)形成受控表并修正金标占位动作，4项检查覆盖动作与范围拒绝。真实原主体、父任务、资源/当前策略仍缺，D-03未关闭。见[复核记录](evidence/operation-policy-review.json)。

## Grant令牌/SDK选型复核（2026-10-13）

[专用JWT与go-jose候选](GRANT_TOKEN_PROFILE.md)固定源码版本、算法、字段、时间及轮换边界；4份源码摘要和官方发布/公告已有依据。未安装或运行TOKEN-01至06，不证明验签、轮换或当前授权，D-03保持开放。

## Grant成熟SDK追加复核（2026-10-13）

[首个Go验签适配器](GRANT_TOKEN_PROFILE.md#6-首个sdk适配器及局部运行证据)使用go-jose/v4固定版本，4项真实临时密钥签发测试覆盖密码学篡改、绑定/算法与整数时间边界。生产信任/服务身份、轮换分发、当前授权、父任务和账本无证据；不能单凭验签许可业务执行，D-03未关闭。见[复核记录](evidence/grant-sdk-runtime-review.json)。

## 公钥快照追加复核（2026-10-13）

[不可变Verifier及key窗口](GRANT_TOKEN_PROFILE.md#7-不可变公钥快照与窗口)新增4项真实签名基础测试。旧实例不会自动更新，部署可信来源、持久代际/防回滚、分发与完整授权仍待验，TOKEN-04及D-03未关闭。见[复核记录](evidence/grant-key-snapshot-review.json)。

## 公钥JSON加载追加复核（2026-10-13）

[快照Schema/跨语言摘要与独立期望](GRANT_TOKEN_PROFILE.md#8-严格json加载与独立部署期望)新增Go3项/Python4项检查。加载通过不证明部署期望可信或版本无回滚；分发/高水位/装配仍待验，D-03/D-04保持开放。见[复核记录](evidence/key-snapshot-load-review.json)。

## 信任激活设计复核（2026-10-13）

[持久高水位与分发恢复](KEY_TRUST_ACTIVATION.md)明确配置来源、原子Head、永久退役/受损指纹、旧实例拒绝及备份外部锚点；6项运行门槛尚未执行。未新增持久事务或真实分发证据，D-03/D-04保持开放。

## 部署信任仓储契约复核（2026-10-13）

[部署域/仓储/事务端口](KEY_TRUST_ACTIVATION.md#6-平台仓储与部署事务端口)以平台类型定义，3项Head推进关系测试不证明持久CAS。Trust记录新增后现有导出库存仍不完整，正式启用前需版本化扩展，D-03/D-04保持开放。见[复核记录](evidence/trust-ports-review.json)。

## 元数据1.1库存追加复核（2026-10-13）

[9类新增权威族](EXPORT_CONTRACT.md)已纳入清单与Journal enum；4项增量检查覆盖相同段数遗漏、旧版本、包络计数与链条。typed载荷/源库存/真实提交导入重放无证据，D-04仍开放。见[复核记录](evidence/export-trust-inventory-review.json)。

## TrustHead类型化记录追加复核（2026-10-13）

[首族payload与Go实际映射](EXPORT_CONTRACT.md)接入导出检查，3项新增测试含独立Schema和Go往返；记录键/高水位错配拒绝。其它payload、Journal类型化和真实来源/导入/CAS缺证，D-04仍开放。见[复核记录](evidence/trust-head-record-review.json)。

## TrustHead类型化Journal追加复核（2026-10-13）

[日志payload及链内推进](JOURNAL_PROTOCOL.md)新增4项检查，完整重算哈希后仍拒绝版本矛盾、倒退、issuer改变和Head墓碑。首条与独立源快照、退役/审批/外部锚点及真实重放仍缺证，D-04保持开放。见[复核记录](evidence/journal-trust-head-review.json)。

## TrustHead独立起点追加复核（2026-10-13）

[Journal起点库存](JOURNAL_PROTOCOL.md)新增4项检查，首条Head必须与独立库存对照，假Bootstrap拒绝且输入不修改。来源认证/完整性、实际冻结包/外部锚点和原子重放仍缺，D-04未关闭。见[复核记录](evidence/journal-trust-baseline-review.json)。

## 导出与Journal起点关联复核（2026-10-13）

[导出Identity/原始清单bytes关联](JOURNAL_PROTOCOL.md)新增4项检查，源typed Head从同一已核验包中提取。独立期望出处、真实源数据库/屏障/库存及外部锚点没有证据，D-04未关闭。见[复核记录](evidence/trust-export-baseline-review.json)。

## 中文关键词候选复核（2026-10-13）

[OpenSearch/SmartCN固定源码设计](KEYWORD_ADAPTER.md)补齐独立keyword通道，4份固定源码及官方版本元数据已有依据。3.3.2仅兼容基线，插件、制品维护、过滤/来源、双实现和Recall/性能没有运行证据，D-05未关闭。

## Fuser及来源轨迹追加复核（2026-10-13）

[排名融合实现](FUSION_CONTRACT.md)新增4项Go和2项独立Schema/实际wire检查，多路轨迹弥补Candidate单路字段不足。获准路线、真实后端/跨KB协调与接口替换、效果/性能仍缺证，ROUTE-02/D-03/D-F未因此关闭。见[复核记录](evidence/fusion-review.json)。

## Fuser获准路线/预算追加复核（2026-10-13）

[ProfileRRF配置绑定](FUSION_CONTRACT.md#6-pipelinequeryprofile绑定的融合实例)新增4项Go检查，未知路线/后端标识/策略覆盖拒绝，必需路线与全局预算核验。配置真实性、服务身份、工厂和实际多KB/后端/替换无证据，ROUTE-02仍未完全通过。见[复核记录](evidence/fusion-profile-review.json)。

## Fuser受控装配追加复核（2026-10-13）

[模块装配与重启](FUSION_CONTRACT.md#7-受控模块装配与重启边界)新增3项Go检查，配置身份与工厂捕获值核对、runtime解析/健康/关闭及配置更换后重建有局部证据。非第二算法替换，未验证可信Profile归一化、多KB装配、真实后端、权限和效果。整体仍有条件通过，V1冻结/运行验收未通过。见[复核记录](evidence/fusion-module-review.json)。

## 跨KB检索协调追加复核（2026-10-13）

[跨库配额与失败语义](RETRIEVAL_COORDINATOR.md)新增纯规划器及5项Go检查；各目标共享路线配额、全局候选与调用数准入、库存排列稳定已有局部证据。实际并发/截止、交错合并、故障降级和授权/发布未执行，新增COORD-01至06运行门槛，ROUTE-02与整体验收保持开放。见[复核记录](evidence/retrieval-plan-review.json)。

## 跨KB合并追加复核（2026-10-13）

[交错合并](RETRIEVAL_COORDINATOR.md#6-合并实现与局部证据)新增6项Go检查，成功结果在配额与目标校验后形成一条路线，乱序稳定且实际RRF不增加路线票数。COORD-03仅算法局部证据，未验证真实后端/来源/代际、执行并发与故障降级/当前权限，ROUTE-02与总体仍开放。见[复核记录](evidence/retrieval-merge-review.json)。

## 检索工作池追加复核（2026-10-13）

[固定工作池与故障分类](RETRIEVAL_COORDINATOR.md#7-固定工作池与故障分类)新增6项受控回调Go检查；并发、join、取消/截止、必需失败、可选整路线降级和预检取得局部证据。COORD-02/04尚缺真实适配器关闭/网络截止及当前权限/发布，ROUTE-02和整体保持开放。见[复核记录](evidence/retrieval-executor-review.json)。

## 执行器预算与混合错误复核（2026-10-13）

[总字节预算与失败分类修正](RETRIEVAL_COORDINATOR.md#8-跨路线字节预算与混合错误修正)补齐跨路线累计限制，并拒绝Join混合错误降级；3项新Go回归及全Go/vet通过。不能证明真实峰值内存、适配器正确分类、当前授权/发布或Recall，COORD与整体验收仍开放。见[复核证据](evidence/retrieval-budget-review.json)。

## 部署调用上限复核（2026-10-13）

[MaxCalls完整传递](RETRIEVAL_COORDINATOR.md#9-部署准入限制完整传递)补齐规划到执行的部署上限，3项Go检查及全Go/vet通过；只证明内部Plan结构/准入，不证明可信配置归一化、权限/发布或真实容量。完整验收保持开放。见[证据](evidence/retrieval-call-cap-review.json)。

## 分库请求授权边界复核（2026-10-13）

[分库请求设计](PARTITION_REQUEST.md)明确Scope不能自行裁剪为新授权、发布投影摘要必须由权威服务提供、最终子请求必须签发新Grant。新增PART-01至06门槛，均未执行；只完成设计，COORD-01/05与整体开放。

## 发布投影身份复核（2026-10-13）

[父子发布投影](PUBLICATION_PROJECTION.md)澄清SnapshotDigest语义与关系记录、过滤集合等式、生成顺序和失效。不声称历史单库摘要已兼容；Schema、摘要金标与权威仓储尚未实现，PART-03/COORD-01/05及整体开放。

## 发布投影机器契约复核（2026-10-13）

[投影Schema与离线关系检查](PUBLICATION_PROJECTION.md#7-首个机器契约与离线检查)新增6项检查，全部167项Python回归通过；只证明闭集结构和给定记录的关系，不证明真实发布/授权、摘要或实际Go请求映射。报告authorized/ready固定false，PART-03和整体保持开放。见[证据](evidence/publication-projection-schema-review.json)。

## 发布投影摘要复核（2026-10-13）

[固定域与Go对照](PUBLICATION_PROJECTION.md#8-固定域摘要与独立go复核)补5组金标/4项新检查，171项Python回归及全Go/vet通过。Go计算器明确schema_validated=false；独立期望来源、真实投影/Scope、实际请求和实时状态仍缺证，PART-03及整体保持开放。见[证据](evidence/publication-projection-digest-review.json)。

## Go投影加载复核（2026-10-13）

[闭集加载与精确整数](PUBLICATION_PROJECTION.md#9-go闭集加载与关系校验)新增3项实际Go/Python检查，174项Python及全Go/vet通过。ValidatedDigest证明结构/给定关系及摘要，不证明父快照/当前授权/发布或实际请求；工具authorized=false，PART-03及整体保持开放。见[证据](evidence/publication-projection-load-review.json)。

## 投影与实际请求复核（2026-10-13）

[RetrieveRequest关系校验](PUBLICATION_PROJECTION.md#10-实际retrieverequest关系检查)新增3项Go检查，含实际wire往返及19种错配拒绝，全Go/vet通过。仅给定投影与请求关系，真实授权/发布、完整Ready/布局、新Grant与应用链未验；PART-01/03及整体保持开放。见[证据](evidence/publication-projection-request-review.json)。

## 查询权威服务复核（2026-10-13）

[QueryAuthority及只读仓储](QUERY_AUTHORITY.md)补齐权威事实来源，明确现有独立元数据读无法证明一致发布库存、外部策略跨系统保证及流式线性化边界。新增AUTHORITY-01至06门槛，均未执行；仅设计交付，PART/COORD与整体保持开放。

## 只读仓储端口复核（2026-10-13）

[Go端口与页面关系](QUERY_AUTHORITY.md#7-首批go读端口与页面关系检查)新增4项检查及全Go/vet通过。仅声明回调/只读仓储并校验给定页面；无真实快照、句柄失效、cursor绑定、当前权限和工厂证据，AUTHORITY及整体保持开放。见[证据](evidence/query-read-ports-review.json)。

## 多页库存收集复核（2026-10-13）

[有界收集器](QUERY_AUTHORITY.md#8-有界多页库存收集器)新增5项Go检查及全Go/vet通过，跨页关系/游标循环/预算拒绝有局部证据。测试仓储不证明真实一致性、句柄、cursor绑定和完整库存；AUTHORITY与整体保持开放。见[证据](evidence/query-read-collection-review.json)。

## 只读句柄生命周期复核（2026-10-13）

[Guard包装](QUERY_AUTHORITY.md#9-回调仓储生命周期包装)新增4项Go检查及全Go/vet通过，包装句柄关闭/合作取消有局部证据。真实WithinRead与底层原始句柄不泄漏、驱动资源/事务释放尚未验证；AUTHORITY-01/05及整体保持开放。见[证据](evidence/query-read-guard-review.json)。

## ReadUnit自动保护复核（2026-10-13）

[自动包装及回调协议](QUERY_AUTHORITY.md#10-readunit自动保护与回调协议)新增7项Go检查，到期成功竞态已重现并修复，全Go/vet通过。仅受控底层组合，不证明真实事务释放/原始句柄不泄漏/工厂或授权一致性，AUTHORITY-01/05/06及整体保持开放。见[证据](evidence/query-read-unit-review.json)。

## 只读能力内核装配复核（2026-10-13）

[query_read_unit装配](QUERY_AUTHORITY.md#11-内核能力装配及元数据归属)新增2项内核检查，两个测试提供方保持接口、特性和类型预检通过，全Go/vet通过。正式部署角色/数据源锁、真实数据库/迁移未实现，AUTHORITY-06与整体保持开放。见[证据](evidence/query-read-module-review.json)。

## 只读部署角色复核（2026-10-13）

[query_read_unit与必需特性](QUERY_AUTHORITY.md#12-部署角色与声明特性补齐)新增3项检查，177项Python回归通过；更新示例check_config仍ready_for_service=false。自报清单与结构校验不证明真实factory/快照/读写数据源锁，AUTHORITY-06和整体保持开放。见[证据](evidence/query-read-deployment-review.json)。

## 元数据读写数据源绑定复核（2026-10-13）

[存储绑定设计](METADATA_STORE_BINDING.md)明确三字段ReadIdentity不足以证明当前权威来源，补独立期望/端点观测、提交点覆盖、共同激活与RPO边界。新增STORE-01至06门槛均未执行，只有设计，AUTHORITY-06与整体保持开放。

## 数据源身份/观测端口复核（2026-10-13）

[Go端口与关系检查](METADATA_STORE_BINDING.md#6-go身份与观测契约)新增4项Go检查及全Go/vet通过；仅给定身份与端点库存，无独立来源/真实探测/提交点覆盖或就绪证明。STORE-01至06及整体保持开放。见[证据](evidence/store-binding-ports-review.json)。

## 数据源绑定离线Schema复核（2026-10-13）

[review包络](METADATA_STORE_BINDING.md#7-离线review机器契约)新增5项检查，182项Python回归通过。只有结构和给定关系，未验证独立期望来源、真实端点、Go映射或提交链；provenance/coverage/ready固定false，STORE及整体保持开放。见[证据](evidence/store-binding-schema-review.json)。

## 绑定记录Go映射复核（2026-10-13）

[显式离线codec](METADATA_STORE_BINDING.md#8-显式go映射与实际往返)新增3项实际Go/Python检查，185项Python回归及全Go/vet通过。仅JSON/字段/给定关系，未探测或激活，不能证明独立来源/覆盖；STORE及整体保持开放。见[证据](evidence/store-binding-wire-review.json)。

## 数据源启动门复核（2026-10-13）

[启动流程设计](STORE_STARTUP_GATE.md)识别当前Observation/Receipt未绑定本次探测，明确新鲜性升级、真实覆盖及最终控制面复核。新增START-STORE-01至06均未执行，仅设计；STORE及整体保持开放。

## Bound探测契约复核（2026-10-13）

[绑定探测Go模型](STORE_STARTUP_GATE.md#7-首批绑定探测go契约)新增4项Go检查及全Go/vet通过；只核对给定绑定/身份/时间，未认证期望摘要或服务，没有一次性账本/Schema/真实覆盖。相同响应重复验证仍可能成立，START-STORE与整体保持开放。见[证据](evidence/store-probe-binding-review.json)。

## Bound探测离线Schema复核（2026-10-13）

[闭集纳秒review](STORE_STARTUP_GATE.md#8-绑定探测离线机器契约)新增5项检查，190项Python回归通过；不是远程协议/签名载荷，Go时间映射、时钟来源、服务认证/覆盖和账本仍待验。报告authenticated/coverage/ready固定false，START-STORE及整体保持开放。见[证据](evidence/store-probe-schema-review.json)。

## Bound探测时间映射复核（2026-10-13）

[Go纳秒codec](STORE_STARTUP_GATE.md#9-绑定探测go时间映射)新增3项实际Go/Python及Go范围检查，193项Python/全Go/vet通过。仅离线关系与精确转换，未认证now/期望或服务、不消费账本；START-STORE及整体保持开放。见[证据](evidence/store-probe-wire-review.json)。

## 探测一次性接收设计复核（2026-10-13）

[接收账本协议](STORE_PROBE_LEDGER.md)明确持久CAS、重复/冲突、控制面栅栏及提交未知恢复，新增PROBE-LEDGER-01至06均未执行。仅设计，非跨进程一次性接收证据，START-STORE及整体保持开放。

## Go探测账本状态复核（2026-10-13）

[状态与事务端口](STORE_PROBE_LEDGER.md#7-go账本状态与事务端口)新增5项Go检查及全Go/vet通过。只是给定记录/租约关系，不执行数据库CAS、认证控制面或整个Check共同接收；无跨进程持久证据，PROBE-LEDGER及整体保持开放。见[证据](evidence/store-probe-ledger-ports-review.json)。

## Check库存集体候选复核（2026-10-13）

[集体接收准备](STORE_PROBE_LEDGER.md#8-check库存与集体接收候选)新增5项Go检查及全Go/vet通过，固定成员与全量候选关系有证据。没有执行CAS、消费账本或事务回滚，多进程与真实覆盖/授权未验；PROBE-LEDGER-01/06及整体保持开放。见[证据](evidence/store-probe-check-review.json)。

## 有限反思业务编排复核（2026-10-13）

[反思与草稿设计](REFLECTION_ORCHESTRATION.md)补齐多查询预算与路线去重、生成输出时机、反馈采纳CAS/审批隔离及两种实现对照，新增六项REFLECTION运行门槛均未执行。只有设计，REFLECT-01与整体保持开放。

## 反思多查询规划复核（2026-10-13）

[共享配额规划](REFLECTION_ORCHESTRATION.md#7-多查询预算规划与配置)新增4项Go/1项配置检查，194项Python回归及全Go/vet通过。仅声明MaxPlanQueries并生成有界候选，实际反思执行/合并/计量、权限与反馈事务未验，REFLECT-01及整体保持开放。见[证据](evidence/refinement-plan-review.json)。

## 同路线多改写合并复核（2026-10-13）

[合并与单路线贡献](REFLECTION_ORCHESTRATION.md#8-同路线多改写合并)新增6项Go检查及全Go/vet通过，含实际RRF去重复与冲突正文拒绝。合成结果不证明真实检索/反思循环、跨路线全局计量或权限，REFLECTION与整体保持开放。见[证据](evidence/refinement-merge-review.json)。

## 整轮多查询调度复核（2026-10-13）

[共享dispatch执行](REFLECTION_ORCHESTRATION.md#9-整轮多查询调度)新增6项受控回调Go检查及全Go/vet通过，基础执行回归未破坏。仍无真实Reflector/检索、跨轮计数、模型费用/当前授权和效果，REFLECTION与整体保持开放。见[证据](evidence/refinement-execution-review.json)。

## 请求内有限反思循环复核（2026-10-13）

[循环/必需Gate](REFLECTION_ORCHESTRATION.md#10-请求内有限反思循环)新增9项受控回调Go检查及全Go/vet通过。不是实际授权/模型/检索，未证明跨调用预算、usage成本或最终效果；REFLECTION与整体保持开放。见[证据](evidence/reflection-loop-review.json)。

## 反思应用接入设计复核（2026-10-13）

[RoundSearch/Gate接入](REFLECTION_PIPELINE_BINDING.md)明确静态Scope刷新缺口、新向量/子Grant、最终证据和全请求计量，新增REF-BIND-01至06均未执行。仅设计，真实闭环和效果仍未验，整体保持开放。

## 全请求计量设计复核（2026-10-13）

[usage与预算设计](REQUEST_METERING.md)识别时间/单轮上限不能证明费用，定义精确计算、预留结算及未知恢复，新增METER-01至06均未执行。仅设计，真实供应商计量与账本仍待验，整体保持开放。

## Go计量算式复核（2026-10-13）

[金额/usage与整数计算](REQUEST_METERING.md#7-go金额usage类型与精确算式)新增6项Go检查及全Go/vet通过，包含大整数/溢出和unknown/估算区别。没有真实费用来源/Reserve/Reconcile或持久并发证明，METER与整体保持开放。见[证据](evidence/metering-arithmetic-review.json)。


## 计量端口与事务规则局部复核（2026-10-13）

新增供应商无关RequestMeter声明与预算、预留、回执关联类型；设计明确同命令重放、CAS、Begin提交后未知发送、unknown回执链和实际超额保留。全Go测试/vet通过，仅类型编译及既有回归证据，无新增运行计量实现。见[证据](evidence/request-meter-port-review.json)。真实授权、持久竞争、恢复和迁移待验，整体有条件通过结论不变。


## 输入分桶局部复核（2026-10-13）

修复uncached关系未验证的缺口：已知子桶必须有已知total，均已知时精确分桶和一致，最大整数不通过加法比较。两项新增测试及全Go/vet通过，见[证据](evidence/metering-input-partition-review.json)。不证明用量来源或实际账本，整体结论不变。


## 静态模块依赖局部验收（2026-10-13）

新增AST检查并核对当前依赖图：87个生产Go源文件符合精确包规则；三项测试验证9类拒绝和允许端口/隔离SDK，全Go/vet通过。见[规范](DEPENDENCY_BOUNDARIES.md)与[证据](evidence/dependency-boundaries-review.json)。代码依赖隔离获得可重复证据，不能推出真实数据隔离或双实现替换；internal/frontend尚未覆盖，整体有条件通过结论不变。


## 统一本地验证入口复核（2026-10-13）

新增可重复的Windows检查入口及失败/缺工具/超时三项测试；全Go/vet、197项Python架构检查和伙伴/管理OpenAPI结构检查通过，库存摘要在运行期保持不变。见[证据](evidence/unified-local-checks.json)。只证明报告列出的本地范围，运行组件/迁移/质量未验，整体有条件通过不变。


## 远程规范统一外部校验（2026-10-13）

补齐统一验证入口对远程17项操作的覆盖，固定openapi-spec-validator 0.9.0验证全部三份规范、47项操作及缺info拒绝对照通过。全Go/vet和197项Python通过。报告绑定当前规范摘要；结构校验不能推出真实身份、Grant或业务语义验收，整体结论不变。


## 统一检查输入库存复核（2026-10-13）

补齐此前遗漏的OpenAPI依赖锁，纳入examples/testdata所有格式字节；两项新增测试验证变更、新增/删除及生成报告不参与输入哈希。统一检查通过，199项Python及Go/vet/三份OpenAPI通过，267项源输入摘要当前有效。工具二进制及安装依赖内容未验证，报告明确该限制；整体运行验收未通过。


## 记忆/图谱生命周期设计复核（2026-10-13）

对照当前Memory/Graph端口，补齐[来源、删除与替换规范](MEMORY_GRAPH_LIFECYCLE.md)，明确确认事件、复合来源、属性证据、投影代际及清理结果等接口缺口。新增7项EXT公共门槛，尚未执行；设计交付不能推出扩展运行能力，阶段G及整体结论不变。


## 记忆来源类型局部复核（2026-10-13）

新增确认事件ID与三类来源关系校验，两项Go测试及统一回归通过。只验证声明标识与类别，不证明确认/任务真实性、来源位置/当前权限或实际Save/Search执行；EXT与整体仍开放。旧缺来源记录明确隔离迁移，不默认为用户确认。


## 记忆记录机器草案局部复核（2026-10-13）

自包含Schema及离线关系校验补齐活动记忆快照，四项Python检查通过，统一回归203项Python及Go/vet/三份OpenAPI通过。仅源声明、Task范围和记录时间/版本形状；Go/wire往返、删除/清理记录和真实权限/来源仍缺，EXT与整体未通过。详见[机器草案](MEMORY_GRAPH_LIFECYCLE.md#9-记忆记录机器草案)。


## 记忆离线Go/wire映射局部复核（2026-10-13）

四项Go测试和两项实际Go/Python样本试验通过，覆盖最大版本、纳秒差、来源/Task和非法输入；统一205项Python及Go/vet/三份OpenAPI通过。仍缺引用多形态完整边缘对照、当前授权、确认真实性与真实服务/删除证明，不关闭EXT或整体验收。见[映射边界](MEMORY_GRAPH_LIFECYCLE.md#10-gowire离线映射与跨语言样本)。


## 记忆wire边缘矩阵复核（2026-10-13）

实际Go/Python对照发现并修复五类差异；八组拒绝和完整引用/可选字段/极端到期样本往返通过。统一207项Python及Go/vet/三份OpenAPI通过。见[回归证据](evidence/memory-wire-edge-review.json)。不证明所有输入等价、实际引用位置或业务当前权限，EXT与整体不变。


## 可选模块最低声明预检（2026-10-13）

Go/Python共享受控清单，memory/graph总计15项最低声明逐项缺失均拒绝；3项Go（含假实现装配与副本保护）和2项Python新增测试，统一209项Python及Go/vet/三份OpenAPI通过。仅声明与类型装配证据，不能证明实际过滤/来源/删除/迁移，EXT与整体结论不变。见[预检规则](MEMORY_GRAPH_LIFECYCLE.md#12-最低声明能力与装配拒绝)。


## 图谱管理端口局部复核（2026-10-13）

新增独立GraphProjectionManager及类型注册，准备/验证和清理绑定平台代际/来源，Activate保留平台CAS事务。两项Go测试验证假实现装配、查询端口冒充被拒绝且关闭；统一回归通过。状态/Schema/wire/正式依赖联结和真实删除、迁移仍缺，EXT与整体不变。见[管理语义](MEMORY_GRAPH_LIFECYCLE.md#13-独立图谱投影管理端口)。


## 图谱查询/管理装配联结局部复核（2026-10-13）

Go/Python检查分离管理依赖及复合自身环，新增3项Go/3项Python试验，统一212项Python及Go/vet/三份OpenAPI通过。只验证假实现的能力/类型与顺序，后端同源、Target及正式启动未验，EXT与整体不变。见[联结规则](MEMORY_GRAPH_LIFECYCLE.md#14-查询与管理的必需装配联结)。


## 图谱管理结果关系局部复核（2026-10-13）

四项Go测试验证Target/Attempt/输入/期望状态绑定及清理回执完成关系；全量统一回归通过。仅局部纯函数，未挂接真实返回路径，不证据真实性、当前授权/保留或清理效果，EXT与整体保持开放。见[结果规则](MEMORY_GRAPH_LIFECYCLE.md#15-图谱管理结果的局部关系校验)。


## 图谱清理操作绑定缺口复核（2026-10-13）

补OperationKind/RequestDigest及独立Expectation，拒绝来源清理/整代删除混用与不同请求摘要；新增Go测试及迁移后的既有测试、统一回归通过。Hash编码与权威期望/账本/身份未完成，不能据此证明实际删除，EXT与整体不变。见[操作绑定](MEMORY_GRAPH_LIFECYCLE.md#16-清理回执的操作类型与冻结请求绑定)。


## 图谱清理语义Hash局部复核（2026-10-13）

闭合DTO+域隔离AKOS-CJSON/1 SHA-256及明确OperationID实现，3项Go测试/统一回归通过。主体/原操作不由哈希认证，跨语言金标、Schema及持久/实际接入尚缺；EXT及整体不变。见[实现边界](MEMORY_GRAPH_LIFECYCLE.md#17-清理语义dto与域隔离摘要实现)。


## 清理语义机器契约/金标归档（2026-10-14）

三项Python试验核对两类独立参考金标和实际Go摘要、顺序/空白等价及歧义/混合输入拒绝；全Go/vet、215项Python、三份OpenAPI通过。293项报告输入摘要当前一致，归档原验证结果，不以旧报告覆盖新输入。[金标与边界](MEMORY_GRAPH_LIFECYCLE.md#18-清理语义schema与独立跨语言金标2026-10-14归档)。仅合成语义证据，真实身份/操作/清理/迁移未验，整体保持有条件通过。


## 版本协商一致性复核（2026-10-14）

修复生命周期前缀额外限制及次版本宿主整数溢出，2项Go/2项Python新增测试和统一回归通过。仅同主版本/最低次版本声明协商；新增可选字段及供应商行为、远程互操作仍需真实契约试验。见[证据](evidence/version-compatibility-review.json)，整体结论不变。


## 特性声明结构局部复核（2026-10-14）

Go内部Factory清单/要求现按标识、长度及唯一性检查，修复空特性和重复要求绕过Schema的差异；2项Go/2项Python新增检查和统一回归通过。合法新名称保留扩展性，仅证明声明一致，不证明特性真实效果，整体结论不变。见[证据](evidence/feature-declaration-review.json)。


## 当前批量读取局部复核（2026-10-14）

助手另开QueryReadUnit回调，范围/数量/总编码字节及关闭规则有3项Go试验，统一回归通过；模拟撤回区别旧/新回调。仅程序调用与边界，真实SQL新鲜度、当前ACL/副本和PreparedSnapshot业务比对仍缺，AUTHORITY与整体不变。见[读取边界](QUERY_AUTHORITY.md#13-另开观察点的有界当前批量读取)。


## 当前事实关系局部复核（2026-10-14）

MatchCurrent核对准备事实与当前批次的身份/发布/索引绑定及完整性，3项Go检查和统一回归通过。只是给定元数据关系，不能证明实时批准/Ready/权限或允许发送；AUTHORITY与整体不变。见[关系边界](QUERY_AUTHORITY.md#14-当前元数据与准备事实的关系比对)。


## 当前复核应用编排设计复核（2026-10-14）

对照真实端口明确当前批准/Revision投影缺口，给出身份/元数据/索引/最终检查与发送边界，区分审核和授权版本。六项CHECK应用门槛未执行；本次为设计交付，不将局部Go函数提升为权威协调服务验收。见[编排规则](CURRENT_CHECK_FLOW.md)，整体结论不变。


## 当前批准投影局部复核（2026-10-14）

CurrentDocumentState增加PublicationReview，MatchCurrentReview先核对发布/代际，再核对冻结版本、批准决定与当前规则评估绑定。缺失投影和未知有效性拒绝；历史批准策略、当前审核规则、ACL版本独立。管理API决定approve与待审记录状态approved区分。

两项Go合成测试覆盖18类投影拒绝、缺失投影、三版本域分离、规则及发布变化。统一Go测试/vet、219项Python及三份OpenAPI通过。局部关系检查不证明内容字节、当前规则来源、批准者权限或仓储一致性；CHECK-01至CHECK-06及总体有条件通过结论不变。


## 刷新身份与范围局部复核（2026-10-14）

MatchRefreshedScope拒绝同策略版本下换主体/发行方/类型/客户/委托/空间、扩大范围或丢失必要来源；允许已验证凭据引用轮换及范围缩小，无证据仍校验身份。三项Go局部测试通过，真实Principal/Scope来源、原值固定和业务调用未验，CHECK-01及总体有条件通过结论保持不变。

本轮统一Go测试/vet、219项Python及三份OpenAPI通过；305项输入SHA复核一致，证据更新至evidence/unified-local-checks.json。仍仅为局部契约验证。


## 局部当前复核编排（2026-10-14）

CheckCurrent将局部关系检查编排为两次控制刷新、新批次读取、批准/冻结核对和真实后端端口核验；输入复制隔离、父deadline不扩大、端口失败静态错误，空证据仍验证身份。四项Go测试覆盖调用顺序、必需阶段失败、核验期间控制变化、空证据与取消。仅模拟端口，真实权威/后端/业务输出挂接及并发发送栅栏未实现；CHECK-01至CHECK-06和总体有条件通过结论不变。

本轮统一Go测试/vet、219项Python及三份OpenAPI通过；307项输入SHA复核一致并更新统一证据。复制前非法UTF-8拒绝，避免JSON修复身份字节。生产运行验收保持未通过。


## 标准冻结内容双摘要核验（2026-10-14）

新增供应商无关FrozenContentReader与contentverify.VerifyFrozen，实际标准JSON与审批绑定的冻结事实比较格式、BlocksHash、ContentHash及平台ID；父预算、超限、取消与静态错误。三项Go替身测试覆盖规范化金标及不符/截断/错版本/超限/读取错误，依赖边界禁止数据库/HTTP进入纯核验包。

尚无真实读取提供者，核验不验证冻结Schema、原件Blob实际字节、索引Ready或候选引用位置。当前CheckOperation无具体Evidence输入，完整引用复核需补此契约；CHECK-03和整体有条件通过结论保持不变。

统一Go测试/vet、219项Python和三份OpenAPI通过；309项输入SHA复核一致，统一证据已更新。生产运行验收未通过。


## 待用证据绑定局部复核（2026-10-14）

CheckOperation增加Evidence并扩展CurrentBackend签名，前置检查证据的发布文档/版本及来源结构；完整嵌套副本传给后端。两项Go测试覆盖冻结转交/隔离与14类非法输入零提供者调用。前次“尚无Evidence输入”的缺口已补；实际Block/Blob归属、定位边界、摘录一致性和真实输出调用未验。CHECK-03/CHECK-06及整体有条件通过保持不变。

本轮统一Go测试/vet、219项Python与三份OpenAPI通过，311项输入SHA复核一致并归档统一证据。生产运行验收未通过。


## 正文证据位置与摘录核验（2026-10-14）

VerifyTextEvidence读取实际标准JSON并比对冻结双摘要，核验revision text块，exact按Unicode码点[start,end)，coarse仅承诺连续子串。三项Go测试覆盖中文emoji、范围副本、10类不符、7类未支持与父预算。当前original/结构化媒体/复合来源定位未实现，未挂真实CurrentBackend；CHECK-03/CHECK-06及多格式验收保持开放，总体有条件通过不变。

统一Go测试/vet、219项Python和三份OpenAPI通过；313项输入SHA复核一致并归档统一证据。生产运行验收仍未通过。


## 可替换证据核验模块设计复核（2026-10-14）

核对现有FrozenContentReader、BlobStore、IndexWriter/CompositeEngine.Verify与Citation/Locator源码后，拆分CurrentBackend组合门面，提出标准内容、原件、索引、格式/定位核验的独立角色。固定路由不允许上传后缀或Agent选择降低核验要求；无状态替换与定位/内容语义迁移区别处理。发现AudioSegment虽有时间范围，Citation.Locator尚无音频时间字段，保留显式契约扩展任务。

6项EV门槛覆盖完整混合批次、双实现、多格式位置、协议兼容、权限/预算及迁移回退，全部未获完整运行证据。本文仅设计，拟定专用端口/特性/配置/远程操作尚未实现，既有313项输入统一验证不为这些提案提供运行证明。总体有条件通过不变。


## 当前新增模块质量修复（2026-10-14）

当前未提交新增模块的审查发现与修复见[MVP_QUALITY_REPAIR](MVP_QUALITY_REPAIR.md)。9项新增Go测试覆盖标准Blob接口、真实原件读取、大小/摘要及namespace限制、Unicode稳定分块、检索前置权限/发布过滤、引用隔离、并发索引、API拒绝未配置/未复核输出及启动边界。依赖允许清单明确新增包的职责，不放开全部第三方依赖。

授权及当前输出复核使用注入端口/替身，Bootstrap未配置业务服务时503；完整真实控制源、元数据事务、幂等账本/崩溃恢复、模块工厂装配、正式REST/MCP与效果验收尚未完成。通过局部修复可继续开发，不等同总体运行验收通过；总体有条件通过保持不变。

本轮统一Go测试/vet、219项Python与三份OpenAPI通过；329项当前源码/Schema输入SHA复核一致，统一证据归档。历史PASS不代表新增模块具备真实授权/事务/迁移能力。


## 解析/分块受控工厂局部复核（2026-10-14）

新增pkg/adapters工厂连接标准Parser/Splitter与已登记内核；解析依赖stream.read的BlobStore，严格配置先校验，健康/关闭状态可观测。两项Go测试通过真实本地文件链路、first/second显式依赖实际Open命中、关闭旧引用及6类非法配置/缺少依赖特性零创建。

测试包含两实例重绑定，不代表两种独立实现迁移。正式Manifest/部署归一化/BuildLock/业务bootstrap与真实授权未接入；总体有条件通过保持不变。

本轮统一Go测试/vet、219项Python及三份OpenAPI通过；331项源码/Schema输入SHA复核一致并归档统一证据，生产验收未通过。


## 本地上传幂等账本局部复核（2026-10-14）

O_EXCL+Sync预留记录，绑定namespace/主体/动作/键及内容承诺，complete结果跨实例重放；已知无效果失败清理，未知状态保留pending并拒绝重复。三项Go回归覆盖重放、语义冲突、主体隔离、并发预留、模拟pending与删除不复活。真实进程退出/断电、目录持久性、自动对账恢复、元数据事务及控制源未验；总体有条件通过和生产验收未通过保持不变。

本轮统一Go测试/vet、219项Python及三份OpenAPI通过；333项输入SHA复核一致，统一证据已归档，生产验收未通过。


## 本地可重试删除局部复核（2026-10-14）

独占硬链接安装已Sync删除记录后清理物理文件，当前Open拒绝记录存在的对象。两项Go测试覆盖跨实例/8路并发删除、数据-only恢复不复活、错误引用/撤销权限及损坏记录。Windows重命名竞争已复现并修复；硬链接不可用时拒绝，生产文件系统准入尚未验。仅本地Blob语义，不代表Document撤回、索引传播、完整恢复或发送栅栏；总体有条件通过不变。

本轮统一Go测试/vet、219项Python及三份OpenAPI通过；335项输入SHA复核一致并归档统一证据，生产验收未通过。


## 内存索引删除局部复核（2026-10-14）

三项Go测试验证代际唯一所属、整代际/分块删除的范围与幂等、晚到Upsert阻止、当前授权拒绝和无效目标无变更。删除标记仅当前进程内存，IndexWriter Prepare/Verify、真实授权、撤回事务/outbox/持久化fencing及跨组件传播未实现；业务闭环和总体有条件通过结论不变。

本轮统一Go测试/vet、219项Python及三份OpenAPI通过；337项输入SHA复核一致并归档统一证据，生产验收未通过。


## 权威撤回事务设计复核（2026-10-14）

实际核对WithdrawRequest/Result、PublicationOperation、TransactionRepositories、EventPayload及IndexDeleteRequest后，发现历史结果load/append及完整固定清理计划尚缺，静态Citation不能表达所有派生来源的形成epoch。补充PublicationHistory/CleanupPlan拟定语义、历史重放、事务原子性、当前权限与旧目标隔离，规定先权威撤回后异步清理。

WTX-01至WTX-06均未执行；新增设计不改变337项机器输入，无代码或SQL服务已实现声明。本地索引删除遇到未知代际不能阻止所有晚到构建，生产仍需权威登记/持久化fencing。总体有条件通过和业务闭环未通过保持不变。


## 发布历史与固定清理计划契约复核（2026-10-14）

两个load/append端口加入TransactionRepositories，历史区分前后epoch与版本，计划绑定撤回前事实及模块/配置/不可变目标引用。三项Go局部测试覆盖历史/计划/渠道覆盖不符。匹配响应不证明权威来源；历史公共DTO、持久化目标定义、SQL原子性、迁移模型和业务特性/准入未实现，WTX及完整治理闭环仍未通过。

本轮统一Go测试/vet、219项Python及三份OpenAPI通过；339项输入SHA复核一致并归档统一证据，生产验收未通过。


## 撤回SQL真实数据库局部复核（2026-10-14）

隔离PostgreSQL 15.17实际运行SQL schema/试验，验证数值、回滚/提交、历史不可变、必需渠道和跨空间/重复目标。运行器核对源码SHA、psql退出码/标记、自身容器清理及镜像/服务器身份；证据为postgres-withdraw-15-review.json，18版本不得由15替代。

缺少Go UoW、真实身份/权限/RLS、审计/日志/任务和全部故障点。普通回归只纳入SQL SHA，不把Docker未执行当数据库验收；WTX及生产验收仍未通过。

本轮统一Go测试/vet、219项Python及三份OpenAPI通过；342项输入（含SQL）SHA复核一致，15.17实际数据库证据SQL SHA匹配。18.0-alpine拉取仍运行（exec session 94179），未创建其数据库验证容器；WTX与生产验收保持开放。


## PostgreSQL运行器失败清理复核（2026-10-14）

已修复docker run创建资源后启动失败且未返回ID时的清理缺口，保存预分配名称并核对自身标签后移除；3项替身测试覆盖远程socket、失败清理与错标签拒绝。15.17实际运行再次通过并清理容器。18.0-alpine拉取会话94179仍运行，未视作停止/重新启动；18数据库及完整WTX验收仍开放。

统一Go/vet、222项Python和三份OpenAPI通过，343项输入SHA一致。15.17证据仍为部分SQL约束，生产/18验证未通过。


## PostgreSQL 18与Go History/Plan片段复核（2026-10-14）

18.0-alpine下载会话94179成功完成；15.17及18.0均实际运行SQL与Go探针，通过映射、跨Scope、句柄关闭、History INSERT回滚及拒绝授权。pkg/postgres只依赖标准database/sql/contracts，pgx v5.11.0由离线cmd探针注册，版本核对官方发布。3项Go单元测试覆盖Scope/期限/关闭、panic锁释放及委托映射。

只提供WithinPublication片段，未声称完整metadata_uow。真实Auth、元数据/审计/outbox/幂等/日志、完整UoW及业务bootstrap仍缺；全部WTX/生产验收保持开放。

统一Go/vet、222项Python与三份OpenAPI通过；348项输入SHA核验一致。15.17/18.0 SQL+Go证据匹配SQL源，验证容器已清理；业务闭环仍未验收。


## Go事务取消与提交前复核（2026-10-14）

新增1项Go测试覆盖VerifyScope返回nil后操作/父context已取消；仓储再次检查并拒绝。15.17和18.0真实探针验证最终复核拒绝时已INSERT History回滚并不持久化。仅使用fixture verifier注入权限变化，未证明IAM/RLS或分布式权限线性化；总体治理/WTX未通过结论保持不变。

统一Go/vet、222项Python及三份OpenAPI通过，348项输入SHA一致，15/18真实提交前拒绝回滚证据已更新。仍不提供生产授权或完整治理证明。


## Go清理计划多目标写入回滚复核（2026-10-14）

真实History.Append/Plan.Append写入计划与index/cache目标后Load验证；主动callback错误或第二目标外键失败均回滚历史/计划头/先前目标。15.17/18.0均实际通过，容器清理，证据新增两个检查标记。种子为合成SQL，不能证明真实治理CAS、权限、审计/outbox/日志；完整WTX及生产验收仍未通过。

统一Go/vet、222项Python和三份OpenAPI通过，349项输入SHA一致，两版实际数据库证据更新。完整治理/WTX未验收。


2026-10-14新增撤回发布头 CAS/outbox 原子事务片段及真实数据库成功/故障探针。架构仍有条件通过，完整治理、真实授权、异构模块替换及 Recall@10 ≥90% 运行验收保持开放。详见 [撤回事务实现](POSTGRES_WITHDRAW_STORAGE.md#8-发布头-cas-与-outbox-原子提交)。


2026-10-14补命令预留与原历史重放：同事务预留回滚、同键摘要冲突和 pending 拒绝接管，重试不创建重复历史/计划/事件。完整命令编码、授权、业务服务和 WTX 验收仍待完成。


2026-10-14补撤回专用审计：同事务写入、历史关系与主体/时间匹配、应用修改/删除拒绝、漏写拒绝及审计失败全回滚；重试不重复审计。完整UoW、生产审计/授权及WTX仍待完成。


2026-10-14撤回应用切片：标准WithdrawalUnit、独立服务及当前头/登记目标读取，真实PG应用提交/历史重放与3项服务单元检查。未注册完整PublicationService；真实Prepare、生产授权、登记fencing和完整WTX仍开放，总体有条件通过。


2026-10-14补撤回摘要与准备器：Go/Python金标、临时字段与稳定身份区分、Verify拒绝/取消、独立ID和拷贝隔离；真实PG应用使用实际摘要代码。身份来源仍fixture，完整生产授权/格式迁移及V1总体验收未完成。


2026-10-14补单库来源登记与清理定义封存：有效来源头/epoch匹配、登记目标双向覆盖、索引定义代际绑定、提交后目标追加拒绝。真实外部生产者/消费者、并发及恢复验证仍开放。


2026-10-14补清理任务存储切片：固定目标进度、租约及递增令牌、终态幂等和unknown停止自动领取。外部效果尚无回执核验/fencing证据，消费者循环、完整任务能力、崩溃和迁移验收保持开放。


2026-10-14补清理单项消费者：稳定操作身份、Lookup优先、独立租约/回执核验和执行准入。4项Go检查及PG存储+编排探针；外部后端仍fixture，生产外部效果、持续投递/恢复及WTX保持开放。


2026-10-14补实际本地索引清理：PG任务/消费者接入MemoryIndex删除与独立状态核验，新代际保留、旧代际重写拒绝。真实权限仍fixture，内存标记非持久化，成熟外部引擎及替换/恢复门槛保持开放。


2026-10-14补索引清理双实现：MemoryIndex与PG共用标准删除/状态读回端口，PG删除标记跨连接保留并阻止旧快照写入。完整检索/投影协议、同评测集迁移/回退与生产数据库恢复仍开放。


2026-10-14补同数据索引交接：完整投影/Owner、代际及分块标记，Memory→PG→新Memory校验值一致；实际SQL恢复故障全回滚。未完成全量元数据/任务/配置迁移、写入追平及部署切换，整体仍有条件通过。


2026-10-14补索引窄能力受控配置装配：内存/PG选择、同快照一致、ConfigVersion变化、特性不足/typed-nil/设置拒绝及旧引用关闭。完整部署/权限/检索切换验收仍开放。


2026-10-14接入MarkItDown实际解析：DOCX/XLSX/PPTX中文和原生文本PDF转换、Go文件存储/进程链路；来源coarse并进入候选。精确结构/扫描OCR、身份、生命周期、第二实现和完整解析门槛仍开放。


2026-10-14补成熟解析器受控工厂：显式BlobStore依赖、宿主转换入口/Health、格式/粗粒度能力和OCR拒绝；四类真实格式由Kernel装配调用，关闭引用拒绝。完整部署/隔离/第二实现及正式解析验收未完成。


2026-10-15追加[审批决定事务](REVIEW_TRANSACTION.md)局部验收：修正待审submitted/版本0契约；三种决定、不可变历史/当前指针、重放/撤权、并发和完整性回滚已在PG15/16/18验证，各通过当前73项检查，21个实际审批投影通过管理Schema。证据为[审批运行记录](evidence/review-runtime-review.json)与最新多版本矩阵。真实权限、当前规则下批准有效性、发布/读取闭环、完整迁移及产品验收仍开放，总体结论不变。


2026-10-15追加[发布准备登记](PUBLICATION_START.md)局部验收：当前规则下批准有效性、配置头写入屏障、操作/任务/outbox原子登记已在三个PG版本通过。当前同一83项检查、18个实际发布投影及9个规则摘要均有证据，见[运行记录](evidence/publication-start-runtime-review.json)。任务执行、索引准备证明、最终发布与读取、真实权限及完整迁移仍未通过；不能用登记成功替代已发布成果，总体验收保持开放。


2026-10-15追加[发布任务租约与已授权读取](PUBLICATION_TASKS.md)局部验收：稳定命令回执、续租/接管隔离、双主体再授权、过期计划失败状态与冻结内容核对在PG15/16/18通过；当前同一96项检查、21个任务投影与12份租约样本有[运行证据](evidence/publication-tasks-runtime-review.json)。实际索引写入隔离、完成/ReadinessProof、最终发布、持续队列和完整TaskRepository仍开放，不以租约与读取证据替代业务执行或总体验收。

2026-10-15追加[实际关键词准备与独立读回](INDEX_PREPARATION.md)局部验收：同数据库租约与写入隔离、持久attempt产物、独立实际投影验证、封存防修改/删除后失效及故障原子性在PG15/16/18通过。当前矩阵每版108项检查、448项同源输入及同一探针；6组历史成功产物/快照通过独立摘要和标准准备DTO Schema，见[准备运行证据](evidence/preparation-runtime-review.json)。统一Go/vet、227项Python与OpenAPI通过。任务完成、最终发布、持续队列、外部引擎fencing、更多通道和第二准备后端替换、真实IAM及生产迁移/质量仍未通过，总体验收保持开放。

2026-10-15追加[准备任务完成](PREPARATION_COMPLETION.md)局部验收：实际候选重新核验、精确租约/版本、原子completed/回执/审计/事件、到期后的历史冷重放、删除/续租/接管/配置变化拒绝，以及提交前撤权/租约耗尽回滚通过。当前同一453项输入和探针在三个PG版本各通过122项检查，6组完成响应/Job投影有[独立证据](evidence/completion-runtime-review.json)。独立Schema发现并修正了新实现的终态名称差异，公共契约保持completed。Go/vet、227项Python与OpenAPI通过；最终发布、持续队列/完整任务仓储、真实IAM、第二后端和生产迁移/召回性能仍未通过，总体验收保持开放。

2026-10-15追加[原子最终发布/替换](FINAL_PUBLICATION.md)局部验收：首次及同Revision重建替换的CAS、当前批准/配置/实际索引复核、旧源清理与正常撤回互操作、撤回后重放不复活、提交前撤权和索引/文档锁均在实库通过。当前同一458项输入与探针在三版PG各通过138项检查；6份结果、12个公共投影及6个命令摘要有[独立证据](evidence/finalization-runtime-review.json)。Go/vet、227项Python与OpenAPI通过。正式有效检索与最终输出复核、新正文换版、真实IAM、第二后端/完整队列清理、生产迁移/产品接口及Recall/性能仍未通过，总体验收保持开放。


## PostgreSQL查询权威读层局部验收（2026-10-15）

2026-10-15最新进展：已实现[PostgreSQL查询权威读层](POSTGRES_QUERY_READS.md)，上层使用标准QueryReadUnit/Repository，同一只读Repeatable Read读取有效发布、当前批准/规则及后端代际，分页游标和句柄限制在回调内。真实撤回、撤销批准、对象隔离、权限回调、取消和store epoch轮换均有实库场景；撤回前的当前元数据比对通过，新读取后明确拒绝旧来源。同一461项输入与同一Go探针在PG15.19/16.15/18.6各通过150项累计检查，三组真实观察投影通过独立关系校验；Go全量测试/vet、227项Python与三份OpenAPI通过，测试实例/目录/端口清理已核对。

下一步贯通正式Retriever、真实索引及正文证据核验与输出前复核。当前实现仅证明元数据读取子集，最多1000个有效发布头；真实IAM、生产装配、第二仓储替换/恢复、REST/MCP/UI与Recall/性能验收继续开放。

[独立查询证据](evidence/query-read-runtime-review.json)只核对元数据读层的真实返回关系，不能证明索引仍存在、证据位置正确或当前输出授权。AUTHORITY与CHECK完整门槛保持开放，总体验收结论不变。


## 当前发布关键词检索局部验收（2026-10-15）

2026-10-15最新进展：已实现[当前发布关键词Retriever](POSTGRES_KEYWORD_RETRIEVER.md)，通过标准Retriever接口调用，可信发布投影与实际当前发布/批准/代际比对后，在同一主库快照核验真实索引并执行SQL查询，返回前重新授权并另开观察点复核。实际准备投影保留Published=false，检索依据平台当前发布头；未发布、撤回、撤销批准或实际索引删除均会拒绝旧请求。独立对照三版PG共9个实际候选、18条原生索引记录、3组发布投影摘要及模块Schema。相同464项输入与同一Go探针在PG15.19/16.15/18.6各通过164项检查；Go全量测试/vet、227项Python及三份OpenAPI通过，测试资源清理已独立核对。

下一步接最终正文/索引证据提供者与当前输出复核，再贯通REST/MCP和产品入口。当前只有关键词候选检索；真实身份及QueryAuthority/Grant、更多召回通道、第二引擎替换、生产规模延迟、中文Recall@10≥90%、迁移回退均保持开放。

[独立检索证据](evidence/keyword-retrieval-runtime-review.json)验证真实候选与投影关系，不构成生产IAM、完整QueryAuthority或最终输出授权；CHECK/Recall/迁移及完整架构验收仍未通过。


## 当前发布冻结正文读取局部验收（2026-10-15）

2026-10-15最新进展：已实现[当前发布冻结正文读取器](PUBLISHED_FROZEN_READER.md)，通过标准FrozenContentReader端口读取真实持久化正文。当前发布/批准准入后，以类型化Revision重新构建标准JSON并逐字节及双摘要比对，返回前新观察点复核；与发布准备任务共享底层内容核验，并保留操作/审核文档及版本一致性检查。三版PG共9份实际正文通过独立Schema/canonical/双摘要及归属验证。相同466项输入与同一探针在PG15.19/16.15/18.6各通过172项检查，Go全量测试/vet、227项Python与三份OpenAPI通过，私有测试资源清理已独立核对。

下一步补具体引用位置与原件字节核验，再接完整CurrentBackend/CurrentControl及产品输出。现有原生引用为original/coarse，既有文本核验器仅支持revision定位；不能改写来源类型来跳过校验。真实IAM、混合召回、第二引擎替换、生产迁移/回退、REST/MCP/UI及Recall/性能继续开放。

[正文运行证据](evidence/published-frozen-runtime-review.json)证明真实当前正文与批准摘要一致，不证明原件真实性、引用位置或最终输出授权。CHECK及完整架构验收保持未通过。


## 当前证据后端实库组合验收（2026-10-16）

2026-10-16最新进展：已实现[PostgreSQL当前证据后端](POSTGRES_EVIDENCE_BACKEND.md)，由CheckCurrent实际串联当前发布/审核、真实关键词索引、类型化冻结正文及本地原件摘录。释放SQL读事务后处理原件，再开新观察点重核实际索引和发布；实库覆盖投影期间删除索引、原件独立拒权、最终控制撤权、人工修订不冒充原件，以及撤回/撤销批准。实库复现并修复分块逻辑删除但物理行仍在时旧核验可通过的问题，检索与证据后端现在均拒绝相关旧代际。相同475项输入与同一Go探针在PG15.19/16.15/18.6各通过183项检查；3组完整实际CheckCurrent、12份冻结正文、3个关键词候选和51条样本索引记录的独立复核通过。Go全量测试/vet、227项Python与三份OpenAPI通过；PG测试目录/端口清理已独立核对。

下一步接生产身份/CurrentControl与统一知识应用服务、模型及REST/MCP输出，扩展PDF/Office等来源投影。当前用户/原件授权与最终控制仍为fixture，模块部署装配、精确定位、第二后端替换、完整迁移回退、Recall@10≥90%和固定条件性能仍待验收。局部实库接入不关闭完整CHECK或总体验收。

[独立后端证据](evidence/evidence-backend-runtime-review.json)只证明当前原生关键词/UTF-8引用范围的真实组合核验，生产CurrentControl、网络输出及跨系统撤销屏障未验证。原件目录在嵌套探针结束后清理并检查；PG目录与端口另行检查。完整架构验收仍未通过。


## 成熟PDF/Office原件投影局部验收（2026-10-16）

2026-10-16最新进展：已将[PDF/Office原件文本投影](OFFICE_ORIGINAL_PROJECTION.md)接入现有受控解析工厂。新增可选original.text及original.format特性，按已准入MIME路由成熟转换器，沿用字节限制、健康与关闭状态。Word/Excel/PowerPoint及普通英文PDF通过真实文件上传、解析、冻结正文/原件引用及虚构摘录拒绝检查；三份PDF×MarkItDown/PyPDF验证同一标准调用与关闭后引用拒绝。当前477项输入的Go全量测试/vet、228项Python和三份OpenAPI通过；三版PG各183项及独立运行复核同步通过，测试资源清理已核对。

这是parser实例的复合能力，未完成独立投影远程协议/部署绑定。Office/PDF样本使用测试批准正文/权限，尚未接数据库审核发布和最终模型/API输出；PG证据仍为UTF-8原件。中文复杂/扫描PDF、OCR、精确位置/表格语义、生产身份/CurrentControl、Recall与性能、迁移回退继续开放。

本轮验证未将复合解析/投影模块计为两项独立细粒度替换，也未把普通英文PDF推广到复杂/中文/扫描PDF；完整架构和产品验收仍未通过。


## 请求绑定的当前控制编排局部验收（2026-10-16）

2026-10-16最新进展：新增[AuthenticatedControl](AUTHENTICATED_CURRENT_CONTROL.md)，绑定已准备操作并通过标准Authenticator、Authorizer及CurrentFactsSource逐次重新认证、授权和读取当前配置/审核规则；不复用旧Principal作为认证结果。凭据副本、认证后清除、关闭后拒绝、日志/JSON脱敏、范围/资源/请求绑定和提供者错误隔离已验证；后端期间撤销凭据会在最终重新认证时阻止继续授权。实库CheckCurrent正向链路已使用该编排，认证/授权提供者仍为fixture，规则/索引/正文/原件实际读取。

同一479项输入与同一探针在三版PG各通过184项检查，独立样本确认认证和授权各两次并保留fixture身份标记；Go全量测试/vet、228项Python、三份OpenAPI及成熟组件回归均通过，PG测试资源清理已核对。下一步接入实际身份与策略/配置来源及统一应用服务；生产IAM/RLS、Grant账本、模块部署、最终模型/API/MCP输出、Recall/性能与迁移回退继续开放。

该项证明强制提供者调用及绑定/失败语义，不证明测试身份和授权提供者等同真实IAM。完整CHECK、架构和产品验收仍未通过。
2026-10-18当前稳定窗口更新：最新625项源码快照（含FactoryRegistry和Profile→kernel工厂解析）通过PG16.15 WSL完整探针，SQL/Go仓储检查与资源清理完成；fixture身份/部署仍使`production_acceptance=false`。见[当前PG证据](evidence/current-postgres16-wsl.json)。
