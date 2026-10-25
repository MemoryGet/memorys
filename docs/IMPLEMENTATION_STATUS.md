2026-10-25当前版本验收：性能优化后的同一源码及交付程序，已重新通过CLI/AI 28项、真实Edge网页13项、备份恢复9项、Windows状态初始化1000次及启动脚本检查。三份实库运行器的应用摘要与交付程序一致，来源/前端摘要一致，各自实例与私有临时资源清理完成；桌面与390px手机截图已检查。Search/Fetch实际公开查询及抓取通过，MCP/历史读取/删除及模块装配的当前实库证据来自326项PG18检查。当前Go/vet、258项Python、三份OpenAPI均通过。旧规范报告已归档，规范路径改为本轮报告，完整逐项矩阵见`.local/acceptance/2026-10-25/SUMMARY.md`。本地功能验收通过；正式检索性能`formal-07`已通过全部54个正常千次格（54,000次成功，最差组P95 1.522秒）、6个有界过载格、1/2/3请求每秒各300秒观察和6次真实问答。1/2请求每秒无拒绝；3请求每秒为813次成功、87次预期限流，无异常失败。2小时稳定性测试已通过：7,200次全部成功，无拒绝、超时或正确性错误，P95为1.164秒；独立资源复核确认本组通过，保守内存中位数增长约3.3 MiB、句柄增长9、数据库连接最多5、最大采样间隔5.49秒。8小时稳定性测试于2026-10-25 17:32上海时间开始，仍需完整运行、最终原始记录及资源独立复核和清理核验；当前矩阵35/39通过。整体验收及生产标志保持未通过，不能由功能或小样本基线通过替代。

2026-10-24交付复核：补齐[本地需求与验收对应清单](architecture/LOCAL_DELIVERY.md)。通过连续压力运行复现 Windows 状态目录提交返回系统码5，真实文件句柄不共享删除权限也稳定复现同一错误。已修复该占用分支：最多8次、等待合计630毫秒的有限重试；持续拒绝保留私有临时状态，目标存在时不覆盖，支持长路径，并在错误中仅保留阶段与系统码。实际占用进程名称未确定，不据此指定某个扫描程序。修复前真实句柄回归失败，修复后短暂/持续占用、并发目标、越界移动、长路径及错误脱敏回归通过；连续初始化1000次通过。当前同一源码快照再次通过 CLI/AI 28项、真实 Edge 网页13项、备份恢复9项，以及全Go/go vet、245项Python和三份OpenAPI，私有测试实例/目录均已清理。最终程序已重建，启动脚本文件检查通过。汇总证据 `.local/architecture-check/local-delivery-audit.json` 记录四份匹配当前源码的报告、网页构建和可执行文件摘要。本地明确列出的开发功能已实现；用户自有文档试用由用户在本机终端启动并核对发布内容。多人管理、OCR、精确定位、质量/规模指标及生产交付不在本地个人交付结论内，`production_acceptance=false`。

2026-10-23前序增量：[离线备份与恢复](architecture/LOCAL_BACKUP.md)新增 `tools/local_backup.py backup/check/restore`，配对保存数据库、完整状态/私钥/原件/缓存、已构建程序和网页、启动与备份工具、解析脚本及依赖锁。备份使用数据库 SHARE 写屏障及状态前后摘要检查；恢复只创建新目录和新数据库，先校验文件与安装绑定，再恢复并核对真实身份/权限/配置。独立 PostgreSQL 18.6 共9项检查通过，含独立连接写入屏障、Windows 私有目录、损坏先行拒绝、现有数据拒绝覆盖及恢复后的真实原件 HTTP 引用和本地语义检索；13项新增单元测试通过。完整 Go/go vet、245项Python及三份OpenAPI最终回归通过，临时资源已清理。首次完整回归曾出现一次 `TestStateIntegrityAndParsing` 状态创建失败；随后定向连续10次及完整重跑通过，原因未复现，保留原始失败报告 `.local/architecture-check/architecture-first-backup-review.json`；后续复现与修复见上方2026-10-24记录。正式运行证据为 `.local/architecture-check/local-backup-postgres18-windows.json` 和 `latest.json`。没有对用户业务数据库执行备份、恢复或其他写入，也没有重试已被拒绝的携带凭据后台启动。当前仍为个人本地能力，跨主版本升级、在线不停写备份和生产灾备未验收。

2026-10-23前序增量：[本地网页工作台](architecture/LOCAL_WEB.md)新增 `memorys-local web`，原 dashboard 模拟数据已替换为真实数据库文档、全文预览、勾选批准且绑定摘要的发布、关键词/本地语义检索、DeepSeek 引用答案及只读诊断。沿用原有数据库和权限链路，个人控制入口仅绑定回环地址；新增会话、Host/Origin、跨站拒绝和静态资源边界。独立 PostgreSQL 18.6 与真实 Edge 浏览器13项验收通过，覆盖草稿不可检索、错误摘要拒绝、DOCX原件引用、真实本地嵌入与 DeepSeek、字面显示文档脚本、手机布局、数据库/原件/状态迁移恢复，以及运行期间禁用所有者后的拒绝读取。28项命令行/AI回归和统一架构检查同时通过，临时实例与目录已清理。最终程序和网页已构建；交互式启动脚本为 `tools/start-local-web.ps1`，不保存输入密码。后台自动启动被工具审批策略拒绝，当前没有启动网页服务。本轮仅通过本地个人功能验收；多人网页登录、OCR、精确引用、规模/质量评测及生产部署继续开放，`production_acceptance=false`。最新运行报告为 `.local/architecture-check/local-web-postgres18-windows.json`、`local-ai-postgres18-windows.json` 和 `latest.json`。

2026-10-23前序增量：[多格式与 AI 扩展](architecture/LOCAL_AI.md)增加 `enable-ai/semantic-search/ask`。PDF、DOCX、XLSX、PPTX 保留真实原件并经所有者明确审批发布；查询分别核对搜索与原件授权。真实 Ollama EmbeddingGemma（768 维、摘要固定）本地排序，DeepSeek Flash 基于获权片段生成带引用答案；生成前后复核当前身份、批准、索引和原件。独立 Windows PostgreSQL 18.6 共28项检查通过，包含四格式真实 HTTP 原件摘录、中文语义排序、真实 DeepSeek、生成中撤权、伪造引用拒绝、原件损坏及数据库/原件/状态/缓存备份恢复。Go全量/go vet、232项Python、三份OpenAPI全部通过。本机已启用并用原有一篇合成示例验证语义检索与问答，未新增业务文档。当前为最多512片/512 KiB的本地扫描，未发布向量索引；网页已由上方后续增量接入，OCR、精确定位、Recall/模型质量及生产规模仍待验收，`production_acceptance=false`。运行记录位于 `.local/architecture-check/local-ai-postgres18-windows.json`、`local-ai-user-db-smoke.json` 和 `latest.json`。

2026-10-23基础闭环：[本地命令行闭环](architecture/LOCAL_CLI.md)提供 `init/serve/import/publish/search/doctor`，复用真实身份、封存 ACL 和提交→个人审批→索引准备→发布链路；支持 UTF-8 TXT/Markdown、摘要绑定发布、幂等重试和任务恢复。基础检查包含各阶段中断、过期租约、撤权、256 KiB 文档、重启、备份恢复和状态目录重定位。本机 `memorys` 库已初始化一篇样例并验证真实 HTTP 搜索。

2026-10-18最新增量：严格BuildLock构造路径现在要求assembler实现`ProductionHealthAssembler`，在启动HTTP/Runner前检查真实DB、IAM和模块健康；检查失败会关闭资源并拒绝启动。当前本地630项门禁通过。

2026-10-18最新增量：严格BuildLock构造路径新增`CoversRuntimeModules`，在调用assembler前核对实际kernel模块目录与锁定provider/release集合精确一致，拒绝缺失、额外或重复模块；本地630项门禁通过。

2026-10-18最新增量：生产Bootstrap新增严格`ProductionLockInput`/`NewProductionRuntimeWithBuildLock`路径，先执行Go BuildLock离线摘要校验，失败时不会调用assembler；成功后传入锁定报告与单次字节快照。当前本地630项门禁通过，PG新轮次仍受宿主时钟门禁影响。

2026-10-18最新增量：新增Go `pkg/buildlock.Verify/VerifyFiles`，对部署/锁/保留策略/模块清单/显式制品执行闭集JSON、原始字节SHA-256、覆盖、生命周期契约、源码revision和制品摘要绑定；报告固定ReadyForService=false，不替代来源信任与生产控制面。当前本地630项门禁通过。

2026-10-18最新增量：`events.RunnerGroup`与`NewRuntimeWithRunnerGroup`支持多个命名outbox消费者的确定性启动、失败回滚、逆序停止和全组Ready；`NewProductionRuntimeWithRunnerGroup`把同一制品快照交给所有Runner。当前本地628项门禁通过；PG新轮次受宿主墙钟跳变拒绝，未覆盖上一份PG证据。

2026-10-18最新增量：Bootstrap新增`NewProductionAuthenticator`，将同一代JWKS、部署issuer/audience、显式subject type和token lifetime绑定到Authenticator，并强制宿主提供真实StatusSource；不生成默认身份或撤销状态。

2026-10-18最新增量：ProductionArtifacts改为每个部署文件只读一次，从同一字节快照解码并记录JWKS/Profile/模块目录SHA-256，避免装配期间重新打开可变路径造成代际混用；本地与PG16当前626项证据通过。

2026-10-18最新增量：删除控制outbox改用PostgreSQL identity-backed `event_seq`排序与固定20位游标，修复随机请求ID作为keyset游标可能跳过并发后写事件的问题；SQL/Go探针与PG16.15当前626项源码快照通过。

2026-10-18最新增量：ProductionArtifactAssembler已接入生产Runtime可选路径；宿主实现该接口时，Bootstrap先加载同一代JWKS/Profile/模块目录并将不可变ProductionArtifacts传入，缺失或不合法制品fail-closed，基础assembler接口保持兼容。

2026-10-18当前稳定窗口更新：加入FactoryRegistry及Profile→kernel可替换工厂解析后的625项源码快照已通过PG16.15 WSL完整探针，临时实例停止与资源清理完成；fixture身份/部署仍使`production_acceptance=false`。见[当前PG证据](architecture/evidence/current-postgres16-wsl.json)。

2026-10-18实库证据：PG16 WSL探针最近稳定窗口通过326项检查且资源已清理；随后为覆盖游标单调约束的复跑捕获约4.83秒墙钟跳变并拒绝，当前完整PG验收保持未通过，production_acceptance仍为false。

2026-10-18当前PG16证据：同一源码快照的WSL PostgreSQL 16.15完整探针再次通过，SQL脚本、Go仓储探针、临时实例停止和资源清理均完成；报告仍明确`production_acceptance=false`，fixture身份/部署不能替代生产验收。见[current-postgres16-wsl.json](architecture/evidence/current-postgres16-wsl.json)。

2026-10-18当前PG16证据更新：加入JWKS装配组件后的619项源码快照再次通过WSL PostgreSQL 16.15完整探针，资源清理完成，`production_acceptance=false`边界保持不变。

2026-10-18当前PG16证据再次更新：加入Profile/模块目录文件加载器后的622项源码快照通过WSL PostgreSQL 16.15完整探针，临时资源清理完成；fixture身份/部署边界和`production_acceptance=false`保持不变。

2026-10-18当前PG16证据更新：加入Profile/模块目录到kernel factory装配路径后的623项源码快照通过WSL PostgreSQL 16.15完整探针，临时资源清理完成；生产验收边界不变。

2026-10-18最新增量：Bootstrap Runtime新增Ready探针，Runner未启动、已停止或最近投递失败时报告不可用；无后台Runner的HTTP组合保持可用。

2026-10-18最新增量：Runner隔离告警钩子panic，告警后端故障不会终止事件消费主循环，也不会改变游标提交和重放边界。

2026-10-18最新增量：Runner新增只读Snapshot，可供Bootstrap健康检查采集游标、启动/停止状态和最近错误，但不被当作业务完成或租约有效性的证明。

2026-10-18最新增量：事件Runner新增可注入OnError告警钩子，投递失败会保留游标并重试，告警接入不改变事件幂等和游标提交边界。

2026-10-18最新增量：Runner将游标持久化失败纳入同一告警边界并保持未提交位置；重试退避响应停止上下文，避免存储故障时阻塞进程退出。新增本地回归覆盖失败重放、告警和快速停止。

2026-10-18最新增量：删除清理控制新增带当前Job版本CAS的显式重试；PG仅允许已知failed目标、无unknown/待处理/有效租约的失败任务恢复，保留目标attempt/fencing单调性并以控制审计/outbox作为状态屏障。PG探针中的重试分支已执行，但本轮整体证据因fixture墙钟跳变保持未通过，production_acceptance仍为false。

2026-10-18最新增量：显式Job重试贯通`JobRetryService`、管理HTTP handler和Bootstrap `/jobs/{id}/retry` 路由；输入严格校验、重新认证/授权、当前版本绑定及 fail-closed 缺失能力均有本地测试。生产身份、实际HTTP重试和完整管理端到端仍开放。

2026-10-18最新增量：Bootstrap新增生产部署设置校验和可替换`ProductionAssembler`生命周期门面；拒绝非TLS数据库、非HTTPS发行方、相对密钥/配置路径和缺失依赖，统一管理Runner启动与assembler关闭。它建立生产装配边界，不证明真实数据库/IAM/Profile/模块目录已部署。

2026-10-18最新增量：新增`pkg/retrieval.RunBenchmark`有界性能Harness，输出并发峰值、P50/P95/P99、失败/超时、候选字节和CostUnits；本地覆盖deadline/取消/计量边界，不产生Recall或商业费用结论。固定中文评测集、真实检索和生产规模性能仍开放。

2026-10-18最新增量：身份装配新增JWKS快照加载器，校验RS256/签名用途/kid唯一性/RSA位数并拒绝私钥与远程密钥地址；可由ProductionAssembler绑定PostgreSQL IdentityStatus。它是生产身份装配组件，不代表发行方、令牌登记和密钥轮换已在真实环境验收。

2026-10-18最新增量：生产文件装配补充`configprofile.LoadFile`和`kernel.LoadConfigFile`，严格拒绝超限、未知字段和尾随JSON，Profile/模块目录可由同一ProductionAssembler按封存快照加载；真实制品签名/目录来源和模块 factory 仍待生产验收。

2026-10-18最新增量：Bootstrap新增`LoadProductionArtifacts`，把JWKS、Profile和kernel模块目录作为同一部署代加载，再交由ProductionAssembler绑定DB/身份状态/factory；依赖边界测试已登记该装配方向，真实制品来源与生产健康仍未验收。

2026-10-18最新增量：`profileassembly.BuildFromFiles`贯通Profile/模块目录文件、宿主factory resolver、阶段绑定与kernel健康/关闭生命周期，补充可替换Retriever装配测试；真实factory来源、制品签名和生产模块健康仍未验收。

2026-10-18最新增量：kernel新增不可变`FactoryRegistry`和provider/release唯一解析，Profile装配可通过`BuildFromFilesWithRegistry`完成受控替换；重复release/未知provider拒绝测试已加入，生产factory来源和制品信任仍开放。

2026-10-18最新增量：PG CursorStore改为单调游标upsert，旧实例覆盖较新游标会失败，避免同一consumer并发/重启时消费位置倒退。

2026-10-18最新增量：Bootstrap新增Runtime生命周期门面，可同时启动/停止注入的事件Runner；退出时等待Runner收敛，HTTP组合与后台消费不再由调用方分别管理。

2026-10-18最新增量：新增`postgres.DeletionControlEventBus`，把删除控制outbox按keyset分页映射为只读EventBus，Runner可直接消费事务事件；发布和任务状态修改仍不允许通过事件总线绕过应用层。

2026-10-18最新增量：HTTP组合根已抽出为`pkg/bootstrap`，集中校验搜索/读取/删除/profile等能力绑定，部分装配失败即拒绝启动，缺失能力继续fail-closed返回503。

2026-10-18最新增量：Runner现在要求注入CursorStore，并仅在游标持久化成功后推进本地位置，宿主重启可恢复消费边界；调度、告警和处理器生命周期仍由Bootstrap负责。

2026-10-18最新增量：事件Runner加入可停止周期调度、每轮deadline和游标边界，正常停止不误报取消，宿主仍负责持久游标和处理器装配。

2026-10-18最新增量：[Outbox事件消费运行时](architecture/EVENT_CONSUMER_RUNTIME.md)新增有界、可重放且回执去重的通用消费者，取消事件可由宿主Bootstrap异步投递；失败不推进游标，具体调度和处理器仍由宿主装配。

# 实施状态

2026-10-18最新增量：[删除清理安全取消控制](architecture/DELETION_CONTROL_RUNTIME.md)新增独立control端口、持久cancelled状态及事务审计/outbox；取消要求当前控制授权、任务未终态且无活跃目标租约，unknown/failed/completed不能被重置，SQL进度门禁保持计数和版本约束。本地契约/PG编译检查通过，管理HTTP取消路径已接入应用层，审计装配和PG时钟实库验收仍开放。

2026-10-18最新本地证据：当前读取装配、幂等时钟监控和健康门禁已完成；最新源码清单609项，Go全量/go vet、232项Python及三份OpenAPI通过。PG16当前实库轮次已通过，历史墙钟跳变诊断仍保留，未把历史PG通过快照升级为当前验收。

2026-10-18当前本地门禁：最新源码清单617项，Go全量/go vet、232项Python和三份OpenAPI均通过，检查期间清单保持不变。生产验收状态仍为false。

2026-10-18当前本地门禁更新：加入JWKS装配组件后源码清单619项，Go全量/go vet、232项Python和三份OpenAPI仍通过，检查期间清单保持不变；生产验收仍为false。

2026-10-18当前本地门禁再次更新：Profile/模块目录加载器加入后源码清单622项，Go全量/go vet、232项Python和三份OpenAPI仍通过，检查期间清单保持不变；生产验收仍为false。

2026-10-18当前本地门禁更新：Profile/kernel factory装配路径加入后源码清单623项，Go全量/go vet、232项Python和三份OpenAPI仍通过，检查期间清单保持不变；生产验收仍为false。

2026-10-18当前本地门禁更新：FactoryRegistry加入后源码清单625项，Go全量/go vet、232项Python和三份OpenAPI仍通过，检查期间清单保持不变；生产验收仍为false。

2026-10-18补充：[运行时墙钟健康门禁](architecture/CLOCK_HEALTH_RUNTIME.md)将墙钟/单调观测做成可复用组件，ConfiguredRuntime要求ClockHealth通过才构造；本地组件、运行时和依赖测试通过。它不改变租约时间或修正系统时钟，生产Bootstrap与PG时钟稳定性仍开放。

2026-10-18最新增量：[当前授权驱动的可替换搜索规划](architecture/CONFIGURED_SEARCH_RUNTIME.md)已加入配置Profile、当前事实/发布清单、父摘要及命名模块解析；本地单元/依赖检查通过，尚未把启动服务标为生产装配。

2026-10-18当前验证状态：[治理时钟诊断](architecture/GOVERNANCE_CLOCK_DIAGNOSTICS.md)。新增有界审批/首次续租检查点及墙钟/单调观测，实际捕获约4.18秒步变；审批提交年龄为负、续租过期的Go/PG值与当前头匹配，业务守卫保持拒绝。正常完整轮次又观测约5.22秒跳变，故当前PG完整验证未通过，不能用此前586项/326检查快照替代。当前590项输入的Go全量/go vet、232项Python及三份OpenAPI通过；诊断禁止伪装完整通过或覆盖标准报告，失败保留源码摘要，五个私有PG实例及端口清理已核对。未修改业务时间守卫、未延长租约、未调整系统时钟；生产时钟健康、审批/续租稳定性及完整V1仍未验收。

2026-10-18最新增量：[上传读取deadline与取消分类修复](architecture/UPLOAD_DEADLINE_RUNTIME.md)。真实HTTP固定读超时先于context定时回调，修复前3/3复现i/o timeout被ctx.Err覆盖成Canceled；新增读取边界分类，仅在实际网络timeout且有效deadline已到时返回DeadlineExceeded。期限内主动取消仍Canceled，Abort错误仍安全ErrCleanup，失败无receipt。六场景各10次共60/60通过，586项输入下Go全量/go vet、228项Python及三份OpenAPI通过；PG16保持326项检查和八组独立实库复核，PG资源清理确认。审批/续租间歇失败及完整V1仍开放；Windows测试空文件已清理，剩余临时目录结构删除被自动审批拒绝，已记录。

2026-10-18最新增量：[清理模块保留绑定与可信装配](architecture/CLEANUP_REGISTRY_RUNTIME.md)。新增标准模块来源、PG封存部署目录及按命名实例解析的注册器，核对旧Profile/目标/代际、实际Provider/实现/契约/特性及kernel配置摘要；逐次重查，撤销、错误实现/配置/Profile和已关闭引用均拒绝。真实service授权下，另一任务通过正确绑定完成实际PG索引fence/零行，负向目标unknown隔离且记录保留；SQL封存/单调head约束及独立摘要/目录/租约/效果复核通过。585项输入在PG16通过326项检查、八组独立复核、Go全量/go vet、228项Python和三份OpenAPI，资源清理已确认。目录发布/来源治理仍fixture；生产部署控制、数据集/制品证明、远程Grant/原子写屏障、对账/Purge及完整V1仍开放。重跑遇到既有续租、审批和上传超时间歇失败，已保留并补充诊断，未认定稳定性问题已修复；本轮仅PG16。

2026-10-18最新增量：[清理worker真实身份与独立授权](architecture/WORKER_AUTHORITY_RUNTIME.md)。新增标准认证/授权控制器及PG当前冻结目标准入，实际RS256 service身份、token/client/key状态及封存ACL反复核查，任务执行与index.delete分别获权；旧策略、改名作用域、跨KB、完成目标、关闭及发现期间撤权均拒绝。只有清理权限的任务failed/unknown且无实际索引删除；独立索引权限齐备后，另一任务自动完成真实fence/零行，失败任务不重试。579项输入在PG16通过317项检查、七组独立复核、Go全量/go vet、228项Python与三份OpenAPI，资源清理已确认。新源/治理仍fixture，生产身份发放/worker装配、模块注册、远程Grant、对账/并发恢复、其他后端/Purge和完整V1仍未验收；本轮仅PG16。

2026-10-18最新增量：[删除队列发现与有界轮询](architecture/DELETION_QUEUE_RUNTIME.md)。新增标准队列端口、PG作用域发现及可取消轮询器；每周期续权，内部keyset游标跨周期保持公平位置，策略变更重置，整页及结果配置核对，终态与有效目标租约任务排除。实库三待处理Job自动完成、实际索引fence/零行、失败unknown无重试、旧策略游标/跨KB/过期/撤权拒绝、取消及其他KB不变通过独立复核。574项输入在PG16通过309项检查，六组独立队列/清理/任务/删除/历史/当前复核、Go全量/go vet、228项Python与三份OpenAPI通过，资源清理已确认。新增任务来源及worker/index授权仍fixture；可嵌入运行循环不等于生产持久调度，生产IAM/装配、并发恢复/对账、其他后端、Purge和完整V1仍开放。初次SQL参数类型失败已保留，本轮仅PG16。

2026-10-18补[删除清理消费者及动态任务状态](architecture/DELETION_CLEANUP_RUNTIME.md)：固定全部已登记历史/当前清理目标，独立运行表及版本门禁，标准协调器每次至多执行一个目标；真实target租约、实际索引删除/回读、聚合完成及GET running/completed/failed贯通。7个实际退役目标、回执摘要/物理事实、完成重放、空任务、unknown隔离、原件/其他来源保留通过独立复核。568项输入在PG16通过302项检查，全部既有读/删除/任务复核、Go全量/go vet、228项Python与三份OpenAPI通过，资源清理已确认。当前PG原生关键词索引及worker授权fixture，不代表持续队列、其他引擎、取消重试/对账、Purge或完整V1已验收。

2026-10-18补[实际删除任务状态查询](architecture/JOB_STATUS_RUNTIME.md)：GET管理契约路径通过独立窄端口和认证应用读取真实Job及资源，PG返回前重读关联并重新申请resource.view_job；kb.delete和正文读取权限均不隐含任务查看。真实查看专用主体、已删除资源、跨KB/缺失/删除专用主体隐藏、读取期间令牌/权限撤销与任务归属不可变通过实库验证。561项输入在PG16通过291项检查，独立Job/Error Schema及实际任务/授权关系、删除/历史/当前回归、Go全量/go vet、228项Python与三份OpenAPI通过，资源清理已确认。只读删除任务初始queued定义，消费者/进度、其他任务类型、取消重试、生产Bootstrap/Grant和完整V1仍未验收。

2026-10-18补[管理删除认证及HTTP接入](architecture/MANAGEMENT_DELETE_RUNTIME.md)：DELETE契约路径严格解析准确文档版本/唯一幂等键且拒绝正文及权限注入；真实kb.delete资源授权映射内部动作，独立控制绑定主体/文档/策略并在事务边界重新认证。标准Job实际登记后返回202；专用删除权限、读取权限拒绝、重放、两类409及令牌/权限撤销通过原生HTTP和独立Schema/Job/ACL复核。553项输入在PG16通过283项检查，Go全量/go vet、228项Python、三份OpenAPI及当前/历史回归通过，资源清理已确认。默认Bootstrap未装配仍503，生产IAM、任务查询/消费、Purge、权限提交写屏障及完整V1保持开放；本轮未重验PG15/18。

2026-10-18补[文档软删除服务及PG事务](architecture/DOCUMENT_DELETION.md)：标准DeleteDocumentRequest/Result及DocumentDeletionStore，内部服务验证稳定意图、行版本和当前资源权限；Serializable事务完成tombstone/历史/回执/审计/outbox及必要清理计划，延迟约束阻止不完整删除。三状态、回滚/恢复、删除后读取与重发拒绝、原件/Revision保留通过实库验证。547项输入在PG16通过275项检查，独立删除/历史/当前复核、Go全量/go vet、228项Python与三份OpenAPI通过，资源清理已核对。治理授权仍fixture，管理DELETE/202投影、生产IAM、Purge、并发/完整故障矩阵及全V1仍未验收；本轮未重验PG15/18。

2026-10-18补[历史读取协议生命周期](architecture/HISTORICAL_READ_RUNTIME.md)：同一真实认证/发布/撤回链路加入REST与MCP，前后两组REST首块到MCP续页通过，撤回及其他版本发布后两端拒绝，显式重发后旧游标返回REST 409/conflict及MCP isError，安全错误无正文/引用/游标。541项输入在PG16通过261项检查，五阶段协议样本独立Schema/实际冻结正文/引用/配置/双格式复核、当前读取回归、Go全量/go vet、228项Python与三份OpenAPI通过，临时资源清理已确认。仍为受控fixture装配，删除、跨issuer/客户端、生产权限/发送屏障及完整V1未验收；本轮未重验PG15/18。

2026-10-17补[历史读取应用撤回/重发游标](architecture/HISTORICAL_READ_RUNTIME.md)：真实认证的CurrentReadService在八个epoch内验证两页读取、撤回拒绝、其他版本发布后仍拒绝、显式重发后新分页及旧游标永久拒绝。四份实际应用页面与冻结正文/块ID顺序独立核对。540项输入在PG16通过258项检查，Go全量/go vet、228项Python、三份OpenAPI和当前读取回归通过，资源清理已独立核对。跨阶段fixture改用主库当前配置摘要，未放宽应用校验；凭据副本退出清除。治理/原件权限仍fixture，删除、跨issuer/客户端、生命周期REST/MCP传输、发送屏障及完整V1保持开放；本轮未重验PG15/18。

2026-10-17补[历史来源撤回与显式重发](architecture/HISTORICAL_READ_RUNTIME.md)：独立有界fixture阶段通过真实Start/Claim/Prepare/Complete/Finalize/Withdraw验证八个连续epoch，目标明确撤回不因其他版本发布而恢复，显式重发目标后以最新发布记录恢复历史读取，正文与批准不变。540项输入在PG16通过255项原生检查与独立实际历史/回执/正文/双授权复核；Go全量/go vet、228项Python、三份OpenAPI和当前读取回归通过，资源清理已独立核对。治理权限仍fixture，删除、应用旧游标、跨issuer/客户端及完整生命周期仍待验；未改变业务预算/4秒任务租约，既有间歇失败仍开放。本轮未重验PG15/18。

2026-10-17补[历史读取跨主体游标隔离](architecture/HISTORICAL_READ_RUNTIME.md)：第二主体的真实令牌、主库登记与独立read/history授权可读取同一历史正文；第一主体游标在第二主体请求下返回ErrReadCursor且无正文/游标。539项输入在PG16通过252项检查，独立身份/授权/正文关系、当前读取回归、Go全量/go vet、228项Python与三份OpenAPI通过，临时资源已独立核对清理。原件授权仍fixture，撤回后重发/删除、跨issuer/客户端及完整生命周期仍开放；已有任务租约间歇失败未关闭。本轮未重验PG15/18。

2026-10-17最新[已发布历史正文读取](architecture/HISTORICAL_READ_RUNTIME.md)：新增标准HistoricalContentReader/HistoricalContentVerifier及可选完整History依赖组，显式旧版本要求当前read和独立knowledge.history.read。PG适配器核对正式发布、当前文档、当前审核规则和原始批准；来源验证每次重读保留历史授权，游标绑定当前与历史快照，签名后再认证/授权及复核历史批准。修复验证基准map浅共享，可变提供方不能同时改写预期与实际；回归先失败后修复。539项输入在PG16通过250项检查，真实同文档换版、旧索引删除后历史原文、REST/MCP继续分页、批准未发布/缺失/更换目标及撤销批准拒绝通过独立复核；当前读取、全量Go/go vet、228项Python与三份OpenAPI通过，测试资源清理已独立确认。本轮未重验PG15/18。治理写入及原件授权仍为fixture，完整历史生命周期、生产装配/Grant/发送屏障、复杂来源和效果/迁移验收仍开放。

2026-10-16最新[按文档读取与签名分页](architecture/PARTNER_READ_DESIGN.md)：应用及PG冻结正文读取器均去掉整库发布扫描；标准ReadCursorCodec端口、本地HMAC实现和应用ReadPage接入REST/MCP。游标绑定身份/委托、权限版本、配置、store epoch、发布/批准/代际、双摘要、块集合和位置；每页重新验证，签名在最终认证/发布复核前。527项输入在PG16通过243项检查，实际两页内容/引用/Schema和双格式完整复核、跨REST游标到MCP继续、篡改和真实令牌撤销拒绝通过；全量Go/go vet、228项Python与三份OpenAPI通过。改正原件预算探针“必须再次缩短”的误判，记录全部实际期限并独立复核；分页样本拆分原文，避免重复索引；查询/输出作为单独20秒fixture阶段，任务租约及业务预算未增加。原件授权仍为fixture，历史/复杂来源/生产密钥与依赖装配/Grant/发送屏障和性能未完成，其他间歇失败仍开放。本轮未重验PG15/18。

2026-10-16最新[当前读取来源验证与REST/MCP](architecture/PARTNER_READ_DESIGN.md)：新增必需且可替换ReadContentVerifier；整块粗定位文本按冻结正文和实际原件逐字验证，结构/derived/精确定位不支持则整次拒绝。来源复核后再次认证/授权及发布检查；REST POST /read与可选MCP knowledge_read复用同一应用。官方SDK客户端读取、严格请求/空块选择/凭据清理及来源故障测试通过。521项输入在PG16通过240项原生检查，独立核对完整原件块、Schema、REST/MCP字段与双格式一致；全量Go/go vet、228项Python及三份OpenAPI通过。原件独立授权仍为fixture，历史/签名游标/正式装配/生产Grant与发送屏障未完成。整体验证先在审核与任务阶段各失败一次，随后通过；失败记录保留，根因未确定，不能称稳定性达标。本轮未重验PG15/18。

2026-10-16新增[MCP搜索运行接入](architecture/MCP_SEARCH_RUNTIME.md)：独立官方SDK适配层、冻结生成目录、严格协议与请求身份隔离，调用统一搜索应用。509项输入在PG15/16/18各235项检查、全量Go/go vet、Python与OpenAPI检查通过；实际MCP工具/响应Schema、双格式及REST证据一致性独立复核已归档。仅搜索工具、受控私有Bearer；其余三工具、OAuth、生产装配与完整产品验收仍开放。

更新：2026-10-16（北京时间）。总目标：开发 AI Knowledge OS 模块化可替换架构设计 V1。

## 当前阶段：契约基础与治理闭环的局部实现

2026-10-16新增[伙伴搜索POST接入](architecture/PARTNER_SEARCH_RUNTIME.md)：契约路径/api/v1/knowledge-os/search映射到统一应用，公开Trace/证据/Error；严格JSON及准确整数校验、认证401和凭据清理。省略KB从主库当前明确授权解析，显式空列表不进入计划且返回前复查凭据；目录区分正常无授权与失效身份/缺失策略。505项输入在三版PG各229项检查，实际HTTP请求/响应独立Schema与来源复核通过，全量Go/go vet、228项Python及三份OpenAPI通过。生产装配、其余伙伴操作及全值域/错误分类仍开放。

2026-10-16补[重排候选预算与Top K](architecture/SEARCH_RERANKING.md)：启用重排时按配置候选预算召回，用户Top K仅限制重排结果。实库中文查询召回两条，请求Top 1且选中原召回第二名；独立匹配实际索引及原件来源。502项输入在三版PG各224项检查，全量Go/go vet、228项Python与三份OpenAPI通过。预算分层不等于成熟模型、Recall/性能验收。

2026-10-16补[替换融合器前置复核](architecture/SEARCH_MULTI_ROUTE.md)：缺失/失败的前置检查阻止Fuser接收候选，复核/融合输入副本隔离；原始Chunk/代际/贡献形状独立校验。实库曾拒绝超出短阶段授权有效期的原件读取，现修正测试原件授权回调收紧deadline，保留内容校验约束。502项输入在三版PG各223项检查及独立调用前后检查计数通过；全量Go/go vet、228项Python与三份OpenAPI通过。远程Grant/发送屏障和成熟组件验收仍开放。

2026-10-16新增[多路由搜索与融合](architecture/SEARCH_MULTI_ROUTE.md)：各路独立候选预算，统一deadline，合并一致的发布/存储事实，默认RRF或标准Fuser替换；检查正文/引用/代际及贡献不能编造。502项输入在三版PG各220项检查，独立RRF分数/贡献与配置摘要复算、Go全量/go vet、228项Python及三份OpenAPI通过。实库两路复用同一PG实现；第二检索引擎、完整多库规划、降级响应及生产产品验收仍开放。

2026-10-16新增[搜索重排与前后证据复核](architecture/SEARCH_RERANKING.md)：启用重排必须提供实现；候选先复核，再调用有预算的重排器，输出只能排序/筛选/截取已知来源，最终再次复核。500项输入在三版PG各217项检查及独立候选/配置关系复核通过；Go全量/go vet、228项Python及三份OpenAPI通过。使用受控测试重排器，成熟组件、效果和多路由仍待验；反思未接入时明确拒绝启用。

2026-10-16新增[统一搜索应用与临时REST](architecture/KNOWLEDGE_SEARCH_APPLICATION.md)：认证/授权、固定计划、装配检索和最终当前证据复核由SearchService统一执行；HTTP只接受公开参数和临时令牌，拒绝客户端权限/过滤器注入。实际SQL候选与localhost HTTP、撤销令牌拒绝通过。498项输入在三版PG各214项检查、全量Go/go vet、228项Python与三份OpenAPI检查通过。单路由、测试计划准入；正式Bootstrap/Grant、伙伴接口、MCP/UI及完整产品验收仍开放。

2026-10-16新增[配置与模块装配](architecture/PROFILE_MODULE_ASSEMBLY.md)：快照按阶段驱动具名绑定检查，支持同能力多个实例/路由；能力、版本、特性不符在创建前失败。实际PG检索已通过配置装配调用，关闭后旧引用拒绝。493项输入在三版PG各211项检查及独立关系复核通过；正式Bootstrap、制品锁、索引阶段与产品集成仍开放。

2026-10-16新增[完整平台配置快照及摘要校验](architecture/PROFILE_SNAPSHOT_INTEGRITY.md)：数据库目录保存Pipeline/Index/Query配置，当前事实源每次重算三层摘要，检索请求使用实际配置摘要。独立Python复算通过，目录篡改、摘要内容不符和旧配置请求拒绝。489项输入在三版PG各208项检查通过；完整模式语义、制品锁和实际模块编译/装配仍待验。

2026-10-16新增[数据库当前事实源](architecture/POSTGRES_CURRENT_FACTS.md)：主库一致性读取当前配置引用、审核规则及授权版本；配置切换后旧请求拒绝、缺失/旧授权拒绝、引用不可修改、指针不可倒退。487项输入在PG15/16/18各205项检查及独立关系验证通过。当前仅验证已登记配置引用，不证明摘要对应真实运行配置；完整目录、编译装配、管理接口和产品入口仍待完成。

2026-10-16新增[持久化授权](architecture/PERSISTENT_AUTHORIZATION.md)：标准Authorizer接入当前封存ACL，按空间/知识库/动作/主体/客户端明确授权，文档目标校验和默认拒绝；策略先封存后切换且禁止版本倒退。真实证据复核已替换测试授权回调，撤权后拒绝。485项输入在三版PG各198项检查及独立实际授权行关系验证通过；管理API/UI、角色、审计、正式迁移和配置源仍未完成。

2026-10-16新增[访问令牌认证与当前状态](architecture/IDENTITY_ACCESS_TOKENS.md)：独立认证适配器使用SDK验证RS256访问令牌，PostgreSQL按发行方/公钥/主体/客户端/令牌登记重新检查当前状态；原生证据链路已接入。483项输入在PG15/16/18各通过192项检查，Go全量测试/go vet、228项Python及三份OpenAPI检查通过。测试发行方、公钥与授权配置不代表生产IAM完成，生产策略、配置装配和产品入口仍待实现。见[本轮证据](architecture/evidence/identity-access-token-review.json)。

当前已贯通[文本关键词准备](architecture/INDEX_PREPARATION.md)、[任务完成/对账](architecture/PREPARATION_COMPLETION.md)与[原子最终发布/替换/撤回](architecture/FINAL_PUBLICATION.md)，已新增[PostgreSQL查询权威读层](architecture/POSTGRES_QUERY_READS.md)，已接入[实际关键词Retriever](architecture/POSTGRES_KEYWORD_RETRIEVER.md)，已新增[当前发布正文读取器](architecture/PUBLISHED_FROZEN_READER.md)，已接入[PG当前证据后端](architecture/POSTGRES_EVIDENCE_BACKEND.md)，生产CurrentControl和产品输出仍待接入。完整架构验收、真实权限、模块替换/迁移及召回性能目标保持开放；以下日期段落保留各轮当时的成果与边界。

2026-10-14补[多格式受控装配](architecture/MARKITDOWN_ASSEMBLY.md)：Parser工厂、BlobStore依赖、版本/依赖Health、能力不足拒绝与关闭引用；四类真实转换通过装配调用。完整部署/OCR/结构/身份及第二实现仍待验。

2026-10-14接入[MarkItDown 0.1.3成熟解析组件](architecture/MARKITDOWN_ADAPTER.md)：四类真实转换及Go/BlobStore进程链路，Office中文/原生文本PDF，粗粒度候选来源明确；OCR、精确定位/结构、真实生命周期与正式解析验收未完成。

2026-10-14补[索引窄能力配置装配](architecture/INDEX_MODULE_ASSEMBLY.md)：受控注册index_cleanup/index_snapshot，配置选择内存/PG，快照一致、旧引用关闭拒绝和特性不足拒绝。生产部署Schema/Bootstrap及完整检索切换仍未完成。

2026-10-14补[同数据索引交接](architecture/INDEX_SNAPSHOT_TRANSFER.md)：标准完整投影/Owner/删除标记快照，Memory→PG→新Memory校验值一致，损坏/覆盖拒绝及实际SQL失败回滚。权威目录为fixture，全量平台迁移/切换和生产验收仍开放。

2026-10-14补[索引清理双实现](architecture/INDEX_CLEANUP_BACKENDS.md)：标准删除/读回端口，通用适配器注入内存或PG；PG删除状态跨连接保留、旧快照写回被拒绝。完整IndexWriter/检索、同数据迁移/回退及数据库重启验证仍待完成。

2026-10-14补[本地索引实际清理适配器](architecture/CLEANUP_NATIVE_INDEX.md)：标准执行器调用MemoryIndex.Delete，独立核验真实Owner/删除标记/记录数；PG任务链路加入实际删除与新代际隔离。权限仍fixture，重启持久化及成熟引擎/替换验收待完成。

2026-10-14补[清理单项消费者](architecture/CLEANUP_WORKER.md)：固定效果身份、Lookup优先、独立租约/回执核验、执行准入及未知结果处理。真实PG存储和编排有探针；外部执行/准入仍fixture，完整消费者和生产验收未完成。

2026-10-14补[清理任务持久化领取](architecture/CLEANUP_TASKS.md)：固定目标进度同事务创建、原子租约/fence与幂等终态、unknown拒绝自动重做；并发实库探针已加入。外部执行/对账、消费者及完整任务能力仍待验。

2026-10-14补[来源登记/计划封存约束](architecture/POSTGRES_WITHDRAW_STORAGE.md#13-来源登记与计划封存约束)：登记必须匹配锁定的有效旧epoch；提交双向核对全部登记目标，封存后不允许追加。消费者、外部投影fencing、并发/恢复与完整WTX仍待验。

2026-10-14补[撤回命令语义与准备器](architecture/WITHDRAW_COMMAND_SEMANTICS.md)：规范化摘要/域隔离、Go/Python金标、可信Verify门槛、独立平台ID及取消检查；PG应用已使用实际摘要。真实身份/文档权限与格式迁移仍待验。

2026-10-14补[撤回应用编排与标准事务端口](architecture/POSTGRES_WITHDRAW_STORAGE.md#11-标准事务端口与撤回应用编排)：业务仅依赖contracts，真实SQL提供当前头/已登记目标，应用流程提交与历史重放已有探针。真实Prepare、完整PublicationService/UoW、登记fencing、迁移及生产准入仍待验。

2026-10-14补[撤回审计原子写入](architecture/POSTGRES_WITHDRAW_STORAGE.md#10-撤回审计与原子性)：审计绑定不可变历史、禁止应用修改/删除；缺失/不匹配拒绝，审计故障全事务回滚。生产审计接口、角色/RLS、迁移日志与完整业务服务仍待验。

2026-10-14补[命令预留/历史重放](architecture/POSTGRES_WITHDRAW_STORAGE.md#9-命令预留与历史重放)：预留与撤回同事务、已提交返回原历史、同键摘要冲突及pending拒绝重做。可信命令摘要编码、真实委托身份和完整应用服务仍待实现。

2026-10-14补[撤回发布头CAS/outbox事务片段](architecture/POSTGRES_WITHDRAW_STORAGE.md#8-发布头-cas-与-outbox-原子提交)：同事务更新发布头、历史/清理计划、事件及命令状态；错误被忽略仍拒绝提交。完整UoW、真实权限、审计与业务闭环待验。

2026-10-14补[清理计划多目标实际写入/回滚](architecture/POSTGRES_WITHDRAW_STORAGE.md#7-清理计划与多目标写入回滚)：History/Plan/两个目标真实插入读取、主动失败和第二目标外键错误全回滚；15.17/18.0均通过。仍使用合成种子，完整权威事务未实现。

2026-10-14补[SQL取消/提交前复核](architecture/POSTGRES_WITHDRAW_STORAGE.md#6-验证后取消与提交前权限变化)：验证返回成功后仍检查父/操作context，提交前撤销回滚实际History INSERT；新增1项Go测试，15.17/18.0真实探针再次通过。IAM/RLS及完整治理闭环未接入。

2026-10-14接入[Go History/Plan SQL片段](architecture/POSTGRES_WITHDRAW_STORAGE.md#5-postgresql-18与go仓储适配片段)：事务Scope/预算/关闭及真实读取、History写入回滚，15.17/18.0实际SQL+Go均通过，临时容器清理。18下载已完成；完整UoW/Auth/审计/outbox/业务bootstrap仍待实现。

2026-10-14补[数据库运行器失败清理](architecture/POSTGRES_WITHDRAW_STORAGE.md)：创建后启动失败时按自身名称/标签清理、错标签及远程socket拒绝；3项Python测试通过，15.17实际路径再次通过。18下载会话94179仍存活；未重复启动，18验收仍未完成。

2026-10-14新增[撤回SQL原型/真实数据库验证](architecture/POSTGRES_WITHDRAW_STORAGE.md)：实际PostgreSQL 15.17约束/回滚试验通过并清理临时容器。18.0标准镜像两次下载截断，同版Alpine下载仍在进行；Go UoW及完整WTX未完成。

2026-10-14补[发布历史/清理计划Go契约](architecture/WITHDRAW_TRANSACTION.md#7-go契约草案与关系检查)：load/append事务端口、前后epoch/固定目标/绑定引用及关系/覆盖检查；3项Go测试通过。SQL、冻结目标定义、导出迁移及业务准入仍待实现，WTX保持开放。

2026-10-14细化[权威撤回事务/派生清理](architecture/WITHDRAW_TRANSACTION.md)：核实缺少可读取历史仓储和固定清理目标，定义历史幂等、CAS/outbox、旧代际隔离和6项WTX门槛。仅实施规格；SQL事务/服务/消费者未实现。

2026-10-14补[内存索引删除/旧任务阻止](architecture/MVP_QUALITY_REPAIR.md#内存索引删除与旧任务阻止)：代际唯一所属、指定代际/分块删除、重复操作及反复写入拒绝；3项Go测试通过。持久化fencing、权威撤回/outbox、重启恢复和真实索引仍待实现。

2026-10-14补[本地可重试删除](architecture/MVP_QUALITY_REPAIR.md#本地删除记录与可重试清理)：独占安装删除记录、拒绝新读取、跨实例继续清理、损坏记录失败关闭及期限绑定；2项Go测试通过。平台撤回/索引传播、完整备份恢复及生产文件系统准入仍待验。

2026-10-14补[本地上传幂等账本](architecture/MVP_QUALITY_REPAIR.md#本地上传幂等账本局部实现)：主体/动作/键及请求摘要绑定、跨实例重放、同键冲突、并发预留与未知结果拒绝重建；3项Go测试通过。自动恢复/清理、断电/进程退出、元数据事务及生产授权仍待验。

2026-10-14接入[解析/分块受控工厂](architecture/MVP_QUALITY_REPAIR.md#解析与分块的受控工厂接入)：标准端口、显式Blob依赖、严格设置、关闭后拒绝旧引用；2项Go集成测试通过。两实例重绑定不等同异构引擎替换，正式部署/BuildLock/业务bootstrap仍待接入。

2026-10-14修复当前新增MVP模块[质量问题](architecture/MVP_QUALITY_REPAIR.md)：统一BlobStore、真实原件解析、受限上传、带引用的前置过滤、稳定分块ID及依赖边界；新增9项Go回归。业务API要求可信Prepare/Verify，默认未配置时503；真实授权、元数据治理及完整运行验收仍待完成。

2026-10-14完善[可替换证据核验模块设计](architecture/EVIDENCE_VERIFICATION_MODULES.md)：拆分内容/原件/索引/专用定位角色，固定路由及声明覆盖，明确音频引用字段缺口、版本迁移和6项EV运行门槛。仅设计，配置示例及专用端口未注册或实施。

2026-10-14补[正文证据位置/摘录核验](architecture/CURRENT_CHECK_FLOW.md#11-正文证据定位与摘录核验)：同次读取实际双摘要后核验revision text块，exact按Unicode码点半开区间、coarse按连续子串；3项Go测试通过。原件/表格/媒体定位明确未支持，真实后端/输出接入仍待验。

2026-10-14补[待用证据冻结绑定](architecture/CURRENT_CHECK_FLOW.md#10-待使用证据的冻结绑定)：Evidence绑定原发布Document/Revision，完整复制后传给后端，非法证据零提供者调用；2项Go测试通过。实际位置/摘录核验与输出调用尚未接入，CHECK-03/CHECK-06保持开放。

2026-10-14补[标准冻结内容读取/双摘要核验](architecture/CURRENT_CHECK_FLOW.md#9-标准冻结内容读取与双摘要核验)：平台ID绑定、实际标准JSON规范化BlocksHash/ContentHash比对、父预算与静态错误；3项Go测试通过。真实读取提供者、原件字节、索引Ready与具体Evidence定位仍待实现。

2026-10-14补[局部当前复核编排](architecture/CURRENT_CHECK_FLOW.md#8-局部编排实现与验收边界)：两次控制刷新、新读取、批准及后端核验按序执行，失败停止、原值隔离及父预算；4项Go模拟测试通过。真实端口、REST/MCP/模型输出接入与CHECK运行验收未完成。

2026-10-14补[刷新主体/范围比对](architecture/CURRENT_CHECK_FLOW.md#7-刷新身份与授权范围的局部比对)：绑定主体、发行方、客户及委托，范围只可缩小并覆盖来源，保留空证据检查和父预算；3项Go测试通过。真实认证/授权源与CHECK-01仍待运行验收。

2026-10-14补[当前批准投影与关系检查](architecture/CURRENT_CHECK_FLOW.md#6-当前批准投影与局部关系检查)：同批次Revision/Approval/Validity，独立审核规则版本，缺失或无效投影拒绝；2项Go测试及统一回归通过。真实权威仓储和CHECK运行验收仍待完成。

2026-10-14细化[当前复核应用编排](architecture/CURRENT_CHECK_FLOW.md)：身份/委托范围、当前批准/冻结关系、真实Ready/引用及最终版本/预算检查；明确现有投影缺口和6项CHECK运行门槛。仅设计，未宣称协调服务已接入。

2026-10-14补[当前元数据/准备事实比对](architecture/QUERY_AUTHORITY.md#14-当前元数据与准备事实的关系比对)：StoreEpoch/策略/发布/Revision/代际/配置关系，拒绝过时/不完整批次，允许无关行版本前进；3项Go测试及统一回归通过。仅元数据关系，实时批准/Ready/权限和真实接入未验。

2026-10-14实现[另开观察点的有界当前批量读取](architecture/QUERY_AUTHORITY.md#13-另开观察点的有界当前批量读取)：自动句柄关闭、ID/数量/编码字节预算、错误零结果；3项Go测试及统一回归通过。已核对PostgreSQL官方隔离语义，真实SQL/ACL/副本新鲜度与AUTHORITY仍未验。

2026-10-14修复[特性声明结构差异](architecture/KERNEL_RUNTIME.md#特性声明结构一致性修复2026-10-14)：Go拒绝空/非法/过长/重复特性和重复要求，保留合法新名称；2项Go/2项Python新增测试，统一219项Python及Go/vet/三份OpenAPI通过。仅结构/声明，实际模块行为仍待验。

2026-10-14修复[版本协商不一致](architecture/KERNEL_RUNTIME.md#版本协商一致性修复2026-10-14)：去掉生命周期1.0额外前缀限制，次版本比较避免宿主整数溢出；2项Go/2项Python新增测试，统一217项Python及Go/vet/三份OpenAPI通过。仅协商声明，真实替换和互操作仍待验。

2026-10-14恢复后归档[清理语义Schema/独立金标](architecture/MEMORY_GRAPH_LIFECYCLE.md#18-清理语义schema与独立跨语言金标2026-10-14归档)：两类金标、实际Go/Python摘要对照与非法输入拒绝；215项Python及Go/vet/三份OpenAPI报告的293项输入哈希当前一致。目标已恢复active；真实操作账本、授权与清理/迁移仍未验。

2026-10-13实现[图谱清理语义DTO/摘要](architecture/MEMORY_GRAPH_LIFECYCLE.md#17-清理语义dto与域隔离摘要实现)：OperationID明确携带，闭合互斥参数、AKOS-CJSON/1+域隔离SHA-256；3项新增Go测试及统一回归通过。缺Schema/跨语言金标、权威原操作/Grant与真实效果，EXT开放。

2026-10-13修复[图谱清理操作绑定缺口](architecture/MEMORY_GRAPH_LIFECYCLE.md#16-清理回执的操作类型与冻结请求绑定)：来源失效/整代删除区分并匹配冻结请求摘要，期望来源独立；新增Go测试、既有关系测试迁移及统一回归通过。摘要编码/持久账本/服务身份与物理效果仍待验。

2026-10-13补[图谱管理结果关系校验](architecture/MEMORY_GRAPH_LIFECYCLE.md#15-图谱管理结果的局部关系校验)：目标/Attempt/输入摘要/状态绑定，清理完成需时间/记录引用，unknown不冒充完成；4项新增Go测试及统一回归通过。未挂接真实管理调用，外部证据/权限/保留/清理效果待验。

2026-10-13补[图谱查询/管理必需装配联结](architecture/MEMORY_GRAPH_LIFECYCLE.md#14-查询与管理的必需装配联结)：分离管理实例/Lookup、复合无自身环，缺失/错误/可选绕过拒绝。3项Go及3项Python新增检查，统一212项Python及Go/vet/三份OpenAPI通过；同源后端/Target和正式归一化仍待验。

2026-10-13补[独立图谱投影管理端口](architecture/MEMORY_GRAPH_LIFECYCLE.md#13-独立图谱投影管理端口)：准备/验证、来源失效、清理状态和整代删除；内核区分查询与管理类型。2项新增Go装配/拒绝测试及统一209项Python、Go/vet/三份OpenAPI通过。仅声明/类型，状态、wire、正式绑定与真实管理行为待验。

2026-10-13补[可选模块最低能力预检](architecture/MEMORY_GRAPH_LIFECYCLE.md#12-最低声明能力与装配拒绝)：同一受控清单供Go内核/Python检查，15项缺失均拒绝；3项Go和2项Python新增检查，全209项Python及Go/vet/三份OpenAPI通过。仅声明门/假实现装配，真实行为与正式部署仍待验。

2026-10-13修复[记忆跨语言边缘差异](architecture/MEMORY_GRAPH_LIFECYCLE.md#11-边缘定位与时间跨语言回归)：五类先失败后修复（零时刻、倒序区间/框、显式空定位文本）；八组拒绝及完整定位往返通过。统一207项Python及Go/vet/三份OpenAPI通过，实际内容/来源/业务授权仍待验。

2026-10-13补[记忆离线Go/wire映射](architecture/MEMORY_GRAPH_LIFECYCLE.md#10-gowire离线映射与跨语言样本)：4项Go检查和2项实际Go/Python往返，整数/时间/来源及闭合字段验证；修复vet构造警告后统一205项Python及Go/vet/三份OpenAPI通过。样本级映射，完整边缘字段/实际服务和EXT仍待验。

2026-10-13补[可选记忆记录机器草案](architecture/MEMORY_GRAPH_LIFECYCLE.md#9-记忆记录机器草案)：自包含Schema复用引用类型、整数版本、任务归属与纳秒到期；新增4项Python测试，统一203项Python及Go/vet/三份OpenAPI通过。非正式Go/wire往返，无真实来源/权限证明，EXT开放。

2026-10-13补[记忆来源Go关系校验](architecture/MEMORY_GRAPH_LIFECYCLE.md#8-来源类型的go局部校验)：确认事件ID及三类来源，缺来源/混合字段/矛盾引用拒绝；2项新增Go测试及统一检查通过。未挂接实际Save/Search或验证确认真实性，EXT保持开放。

2026-10-13细化[记忆/图谱生命周期](architecture/MEMORY_GRAPH_LIFECYCLE.md)：权威与投影归属、任务/主体授权、复合来源整体可读、撤回/TTL/墓碑、删除传播及替换回退；标出当前端口缺口并新增7项EXT门槛。仅设计，无真实扩展运行证据，阶段G保持待实施。

2026-10-13补齐[统一检查输入库存](architecture/VALIDATION.md#统一本地验证入口windows)：依赖锁及所有examples/testdata字节纳入，新增/删除/变更可检出；新增2项测试，199项Python及Go/vet/三份OpenAPI通过。报告绑定267项输入，工具二进制及安装依赖内容未验证。

2026-10-13将远程模块17项操作纳入[统一外部OpenAPI检查](architecture/VALIDATION.md#7-openapi规范校验证据)：伙伴4/管理26/远程17均通过固定校验器，统一入口全Go/vet及197项Python通过。仅规范结构与本地契约，真实联调仍待验。

2026-10-13增加[统一本地验证入口](architecture/VALIDATION.md#统一本地验证入口windows)：Go测试/vet、197项Python检查及伙伴/管理OpenAPI结构校验通过，命令失败/超时/工具缺失不会跳过；报告绑定输入哈希并检查运行期变化。仅本地契约回归，正式运行验收仍待完成。

2026-10-13增加[静态依赖边界门](architecture/DEPENDENCY_BOUNDARIES.md)：87个pkg/cmd非测试Go文件检查通过，3项测试含9类违规拒绝；全Go/vet通过。阻止领域/编排耦合SDK、SQL/HTTP及具体实现；未覆盖未来internal/frontend和实际替换运行，整体仍开放。

2026-10-13修复[输入用量分桶关系](architecture/REQUEST_METERING.md#9-输入用量分桶关系复核)：拒绝uncached超额、缺total及三桶矛盾，采用减法防溢出；新增2项测试，全Go/vet通过。仅局部关系验证，真实计量门槛保持开放。

2026-10-13补齐[计量应用端口与事务边界](architecture/REQUEST_METERING.md#8-计量应用端口与持久事务边界)：Open/Reserve/Begin/Reconcile/Abort/Load声明、版本/幂等及未知回执链设计；全Go/vet通过。无持久账本、供应商对账或并发/恢复证明，METER保持开放。

2026-10-13补齐[Go usage/金额与精确算式](architecture/REQUEST_METERING.md#7-go金额usage类型与精确算式)：6项关系/大整数检查及全Go/vet通过，未知/估算不冒充完整实测、缓存不重复收费。无真实预留/结算/来源证据，METER开放。见[证据](architecture/evidence/metering-arithmetic-review.json)。

2026-10-13补齐[全请求usage/费用计量设计](architecture/REQUEST_METERING.md)：明确全调用关联、整数费率、预留/结算、未知占额、并发和恢复；新增6项METER门槛。仅设计，无真实计量器/费用账本/供应商证据，整体开放。

2026-10-13补齐[反思检索应用接入设计](architecture/REFLECTION_PIPELINE_BINDING.md)：区分初始/追加轮、策略变化终止、新文本嵌入/子投影Grant、来源与最终证据复核及全请求计量；新增6项REF-BIND门槛。仅设计，真实适配器/计量/端到端未验，整体开放。

2026-10-13补齐[请求内有限反思循环](architecture/REFLECTION_ORCHESTRATION.md#10-请求内有限反思循环)：9项Go检查覆盖轮数/统一截止、重复/无新证据、必需Gate、模型隔离与返回复核，全Go/vet通过。受控回调，无真实模型/授权/费用或持久预算证明，REFLECTION开放。见[证据](architecture/evidence/reflection-loop-review.json)。

2026-10-13补齐[整轮反思多查询调度](architecture/REFLECTION_ORCHESTRATION.md#9-整轮多查询调度)：基础与反思复用dispatch池/预算策略，6项新Go检查及全Go/vet通过。只受控SearchFunc一轮，真实Reflector/后端、跨轮/模型计量和授权未验，REFLECTION开放。见[证据](architecture/evidence/refinement-execution-review.json)。

2026-10-13补齐[同路线多改写合并](architecture/REFLECTION_ORCHESTRATION.md#8-同路线多改写合并)：6项Go检查覆盖追踪/稳定顺序、单路线RRF投票、字节预算及冲突证据拒绝，全Go/vet通过。真实执行/循环/计量、跨路线汇总与权限未验，REFLECTION开放。见[证据](architecture/evidence/refinement-merge-review.json)。

2026-10-13补齐[反思多查询预算规划](architecture/REFLECTION_ORCHESTRATION.md#7-多查询预算规划与配置)：MaxPlanQueries进入Go/部署Schema，4项Go及1项配置检查，全部194项Python/全Go/vet通过。仅候选规划，真实执行/合并/计量与权限/反馈未验，REFLECT-01开放。见[证据](architecture/evidence/refinement-plan-review.json)。

2026-10-13补齐[有限反思与反馈草稿编排](architecture/REFLECTION_ORCHESTRATION.md)：明确Assess/Refine/Propose边界、多查询共享预算、流式/缓冲自检及采纳CAS审核；新增六项REFLECTION门槛。只有设计，预算字段/跨查询合并/真实模型与事务未验，REFLECT-01开放。

2026-10-13补齐[Check库存集体接收候选](architecture/STORE_PROBE_LEDGER.md#8-check库存与集体接收候选)：固定成员/Probe/端点及全量候选，5项Go检查与全Go/vet通过。候选不执行CAS或消费账本，真实原子提交/并发仍未验，PROBE-LEDGER开放。见[证据](architecture/evidence/store-probe-check-review.json)。

2026-10-13补齐[Go探测账本状态/事务端口](architecture/STORE_PROBE_LEDGER.md#7-go账本状态与事务端口)：5项关系检查覆盖状态顺序、终态、摘要不变、栅栏/截止及版本溢出，全Go/vet通过。无真实CAS/授权/集体Check/持久恢复证据，PROBE-LEDGER开放。见[证据](architecture/evidence/store-probe-ledger-ports-review.json)。

2026-10-13补齐[探测一次性接收协议](architecture/STORE_PROBE_LEDGER.md)：定义唯一挑战、状态机、CAS/审计/outbox原子提交、重复与冲突、fencing接管和未知提交恢复；新增6项PROBE-LEDGER门槛。仅设计，持久记录/摘要/事务未实现，整体开放。

2026-10-13补齐[Bound探测Go时间映射](architecture/STORE_STARTUP_GATE.md#9-绑定探测go时间映射)：3项实际Go/Python检查及1项Go时间范围检查，全部193项Python/全Go/vet通过。离线now输入不是可信时钟，真实认证/账本/覆盖未验，START-STORE开放。见[证据](architecture/evidence/store-probe-wire-review.json)。

2026-10-13补齐[Bound探测离线Schema](architecture/STORE_STARTUP_GATE.md#8-绑定探测离线机器契约)：闭集包络与精确纳秒关系检查，5项新测试及全部190项Python回归通过。认证/覆盖/ready固定false；Go时间映射和真实账本/服务未验，START-STORE开放。见[证据](architecture/evidence/store-probe-schema-review.json)。

2026-10-13补齐[Bound探测Go契约](architecture/STORE_STARTUP_GATE.md#7-首批绑定探测go契约)：4项Go检查绑定检查/探测/调用、部署授权及精确时间窗口，全Go/vet通过。旧离线格式保留；真实性、期望摘要认证、一次性账本、Schema/映射与覆盖未验，START-STORE开放。见[证据](architecture/evidence/store-probe-binding-review.json)。

2026-10-13补齐[数据源启动核对设计](architecture/STORE_STARTUP_GATE.md)：发现Observation未绑定本次请求，明确探测/回执新鲜性扩展、独立期望、真实覆盖、最终绑定复核及联合就绪；新增6项START-STORE门槛。仅设计，现有端口须升级，整体开放。

2026-10-13补齐[绑定记录显式Go映射](architecture/METADATA_STORE_BINDING.md#8-显式go映射与实际往返)：3项实际Go/Python检查及全部185项Python/全Go/vet通过。离线往返不探测、不激活；真实独立来源/覆盖未验，STORE开放。见[证据](architecture/evidence/store-binding-wire-review.json)。

2026-10-13补齐[数据源绑定离线Schema](architecture/METADATA_STORE_BINDING.md#7-离线review机器契约)：5项新检查及全部182项Python回归通过。报告provenance/coverage/ready固定false；不是已签名期望或正式Go映射，真实控制面/探测/覆盖未验，STORE开放。见[证据](architecture/evidence/store-binding-schema-review.json)。

2026-10-13补齐[Go数据源身份/观测端口](architecture/METADATA_STORE_BINDING.md#6-go身份与观测契约)：4项Go关系检查及全Go/vet通过；读写可不同实例但身份/库存必须匹配，游标覆盖另设端口且未实现。真实控制面/探测/Schema/覆盖均未验，STORE开放。见[证据](architecture/evidence/store-binding-ports-review.json)。

2026-10-13补齐[元数据读写数据源绑定](architecture/METADATA_STORE_BINDING.md)：区分dataset/store epoch/索引epoch、独立控制面与端点观测，明确水位不能字符串比较、共同激活及RPO边界；新增STORE-01至06门槛，尚无机器契约或真实运行证据。整体开放。

2026-10-13补齐[只读部署角色](architecture/QUERY_AUTHORITY.md#12-部署角色与声明特性补齐)：Schema要求query_read_unit与两项特性，更新离线示例；3项新检查及全部177项Python回归通过，配置报告ready_for_service=false。真实factory、数据源锁/归一化仍未验，整体开放。见[证据](architecture/evidence/query-read-deployment-review.json)。

2026-10-13补齐[只读模块内核装配](architecture/QUERY_AUTHORITY.md#11-内核能力装配及元数据归属)：query_read_unit能力/类型检查，2项内核检查覆盖两个测试提供方、生命周期及特性/错误端口拒绝，全Go/vet通过。正式部署角色、数据源锁及真实库迁移未验，整体开放。见[证据](architecture/evidence/query-read-module-review.json)。

2026-10-13补齐[ReadUnit自动保护](architecture/QUERY_AUTHORITY.md#10-readunit自动保护与回调协议)：自动Guard/Close及同步恰一次回调，7项Go检查含到期竞态修复，全Go/vet通过。受控底层单元，无真实事务/工厂/授权一致性证据，AUTHORITY开放。见[证据](architecture/evidence/query-read-unit-review.json)。

2026-10-13补齐[只读句柄生命周期包装](architecture/QUERY_AUTHORITY.md#9-回调仓储生命周期包装)：4项Go检查覆盖关闭旧引用、取消/join、范围副本及有效期；全Go/vet通过。包装器须由真实WithinRead回调正确装配，驱动取消/事务释放未验，AUTHORITY-01/05开放。见[证据](architecture/evidence/query-read-guard-review.json)。

2026-10-13补齐[有界多页库存收集](architecture/QUERY_AUTHORITY.md#8-有界多页库存收集器)：5项Go检查验证固定观察点、跨页重复/游标循环、预算不截断和取消/读失败零部分输出；全Go/vet通过。测试仓储，不证明真实快照/cursor/库存完整性，AUTHORITY开放。见[证据](architecture/evidence/query-read-collection-review.json)。

2026-10-13补齐[Go只读快照仓储端口](architecture/QUERY_AUTHORITY.md#7-首批go读端口与页面关系检查)：观察点/发布页面/当前状态类型和回调接口，4项页面关系检查及全Go/vet通过。真实读事务、cursor绑定/句柄失效、来源与工厂未验，AUTHORITY及整体开放。见[证据](architecture/evidence/query-read-ports-review.json)。

2026-10-13补齐[查询权威服务](architecture/QUERY_AUTHORITY.md)：定义可替换只读快照仓储、Prepare/子授权/投影与CheckCurrent，明确外部策略跨系统一致性和流式输出线性化边界；新增6项AUTHORITY运行门槛。仅设计，真实读端口/仓储/服务未实现，整体开放。

2026-10-13补齐[实际检索请求关系校验](architecture/PUBLICATION_PROJECTION.md#10-实际retrieverequest关系检查)：投影摘要、Scope/过滤集合/代际、身份、预算与有效期核对；3项Go检查含真实modulewire往返及19种错配拒绝，全Go/vet通过。合成权威输入，真实授权/发布及接入应用未验，PART-01/03开放。见[证据](architecture/evidence/publication-projection-request-review.json)。

2026-10-13补齐[Go投影闭集加载](architecture/PUBLICATION_PROJECTION.md#9-go闭集加载与关系校验)：Load/Validate/ValidatedDigest及显式validate工具模式，3项新增实际Go/Python检查；全部174项Python/全Go/vet通过。authorized=false，真实快照和RetrieveRequest映射未完成，PART-03开放。见[证据](architecture/evidence/publication-projection-load-review.json)。

2026-10-13补齐[发布投影固定域摘要](architecture/PUBLICATION_PROJECTION.md#8-固定域摘要与独立go复核)：5组Python/实际Go金标、4项新增检查及全部171项Python/全Go/vet通过。Go工具schema_validated=false，尚无Go领域映射、权威快照与实时状态证据，PART-03开放。见[证据](architecture/evidence/publication-projection-digest-review.json)。

2026-10-13补齐[发布投影机器契约](architecture/PUBLICATION_PROJECTION.md#7-首个机器契约与离线检查)：闭集Schema及离线关系校验，6项新测试与全部167项Python回归通过。authorized/ready固定false；摘要、实际DTO过滤映射、权威仓储/当前状态未验，PART-03开放。见[证据](architecture/evidence/publication-projection-schema-review.json)。

2026-10-13补齐[发布投影身份](architecture/PUBLICATION_PROJECTION.md)：区分父发布摘要与子过滤投影摘要，定义闭集关系记录、请求集合等式、精确摘要顺序及一致性/失效边界。仅设计，Schema/金标/仓储及实时状态证据未验，PART-03和整体开放。

2026-10-13补齐[分库请求与授权边界](architecture/PARTITION_REQUEST.md)：明确目标集合、子Scope来源、发布投影身份、模型/布局核验、新Invocation与Grant重新签发，新增PART-01至06运行门槛。当前为设计交付，权威仓储/投影记录与调用适配器未实现，COORD-01/05及整体开放。

2026-10-13补齐[部署调用上限传递](architecture/RETRIEVAL_COORDINATOR.md#9-部署准入限制完整传递)：Plan保留MaxCalls，执行器预检部署上限与并发关系；3项新Go检查及全Go/vet通过。正式配置来源、真实后端与权限/发布仍未验，目标开放。见[证据](architecture/evidence/retrieval-call-cap-review.json)。

2026-10-13修正[执行器总字节预算与混合错误](architecture/RETRIEVAL_COORDINATOR.md#8-跨路线字节预算与混合错误修正)：3项新Go回归，跨路线累计16MiB且降级不退款，混合契约错误不得作为传输降级；两缺口已重现并修复，全Go测试/vet通过。序列化预算不等于峰值内存，真实运行验收仍开放。见[证据](architecture/evidence/retrieval-budget-review.json)。

2026-10-13新增[检索工作池与故障分类](architecture/RETRIEVAL_COORDINATOR.md#7-固定工作池与故障分类)：6项Go检查覆盖固定并发、取消/join、deadline、必需失败与可选整路线降级及非法Plan；全Go回归/vet通过。受控回调实验，不保证忽略context的适配器截止；真实后端、权限/发布与效果未验，COORD-02/04局部推进。见[执行证据](architecture/evidence/retrieval-executor-review.json)。

2026-10-13新增[跨KB交错合并](architecture/RETRIEVAL_COORDINATOR.md#6-合并实现与局部证据)：6项Go检查覆盖乱序稳定、单路线RRF票数、目标/配额/候选校验、取消与副本；全Go回归/vet通过。仅合成成功结果，尚无实际执行/故障降级、权限/发布/代际真实性或后端/Recall，COORD-03局部推进，整体验收开放。见[执行证据](architecture/evidence/retrieval-merge-review.json)。

2026-10-13新增[检索协调器设计与配额规划](architecture/RETRIEVAL_COORDINATOR.md)：按路线共享跨KB候选预算，目标不能被静默跳过，调用数准入、固定排名合并及必需失败/可选整路线降级规则成文；5项规划Go检查、全Go回归/vet通过。实际工作池、合并、授权/发布和真实后端未验，ROUTE-02与整体验收开放。见[执行证据](architecture/evidence/retrieval-plan-review.json)。

2026-10-13新增[RRF受控模块装配](architecture/FUSION_CONTRACT.md#7-受控模块装配与重启边界)：3项Go检查验证配置身份、注册端口、重建摘要、健康与停止/独立生命周期；全Go回归/vet通过。正式Profile真实性、多KB工厂、第二算法及真实后端/效果未验，整体验收开放。见[执行证据](architecture/evidence/fusion-module-review.json)。

2026-10-13新增[配置绑定ProfileRRF](architecture/FUSION_CONTRACT.md#6-pipelinequeryprofile绑定的融合实例)，捕获获准路线/实例/预算和Pipeline摘要，拒绝额外路线、后端身份字段错配及策略覆盖；4项新Go检查通过。真实性来源/工厂、必需失败与可选降级、多KB调度及实际后端/替换未验，ROUTE-02局部推进，整体验收开放。见[执行证据](architecture/evidence/fusion-profile-review.json)。

2026-10-13新增[首个RRF融合实现](architecture/FUSION_CONTRACT.md)，精确排名比较、同路线/候选去重及FusionOrigins完整来源；4项Go测试和2项独立Schema/Go映射检查通过。Candidate草案与远程Schema新增可选轨迹；获准路线、真实后端/权限、替换与Recall/性能未验，ROUTE-02及整体验收开放。见[执行证据](architecture/evidence/fusion-review.json)。

2026-10-13补齐[中文关键词独立适配](architecture/KEYWORD_ADAPTER.md)，固定核对OpenSearch 3.3.2许可/SmartCN构建与注册/search规格，明确与Qdrant/平台融合的分工、元组过滤、批量准备和6项运行门槛。当前官方latest元数据为3.9.0；3.3.2只作兼容基线，未部署/安装插件或验证召回/维护，D-05开放。

2026-10-13补齐[导出Identity→TrustHead Journal起点](architecture/JOURNAL_PROTOCOL.md)，绑定8项独立期望及清单原始摘要，从同一校验bytes提取typed库存并核对日志起始游标；4项新检查通过。控制面来源、真实写屏障/全库快照和数据库重放未验，D-04开放。见[执行证据](architecture/evidence/trust-export-baseline-review.json)。

2026-10-13补齐[TrustHead独立Journal起点](architecture/JOURNAL_PROTOCOL.md)，要求显式起始库存、游标关联、首条row/high_water与Bootstrap核对，不修改输入；4项新检查通过。真实来源/完整库存、ExportManifest/控制面锚点及数据库重放未验，D-04开放。见[执行证据](architecture/evidence/journal-trust-baseline-review.json)。

2026-10-13补齐[TrustHead类型化Journal](architecture/JOURNAL_PROTOCOL.md)，核对payload/声明版本、Bootstrap与链内高水位/issuer推进，拒绝清除Head的墓碑；4项新测试通过。首次源快照、可信锚点及数据库原子重放未验，ready_for_replay仍false，D-04开放。见[执行证据](architecture/evidence/journal-trust-head-review.json)。

2026-10-13新增[TrustHead类型化导出](architecture/EXPORT_CONTRACT.md)：实际Go字段映射/精确编解码、独立Schema及导出payload/record_id校验，3项Python测试含真实Go往返。其它记录族、Journal payload、真实来源/CAS/导入尚未验证，ready_for_import仍false，D-04开放。见[执行证据](architecture/evidence/trust-head-record-review.json)。

2026-10-13扩展[元数据1.1导出/Journal库存](architecture/EXPORT_CONTRACT.md)：新增上传操作及部署信任9族，空段仍需完整列出，旧1.0声明拒绝；4项增量测试通过。typed payload、真实源库存/导入/重放和高水位锚点仍待验，D-04开放。见[执行证据](architecture/evidence/export-trust-inventory-review.json)。

2026-10-13新增[部署信任仓储端口](architecture/KEY_TRUST_ACTIVATION.md#6-平台仓储与部署事务端口)，独立DeploymentScope/Authorizer和TrustRepository/UnitOfWork，共同提交Head/操作/退役/审计/outbox/journal；3项版本关系测试通过。真实CAS/事务、部署授权及导出库存扩展未实现，D-03/D-04开放。见[执行证据](architecture/evidence/trust-ports-review.json)。

2026-10-13补齐[公钥信任激活、防回滚与恢复](architecture/KEY_TRUST_ACTIVATION.md)，明确TrustHead/高水位、退役公钥指纹、事务提交及分发重启边界，历史备份恢复须独立控制面锚点；6项TRUST运行门槛待执行。本次为设计交付，仓储、机器记录、BuildLock扩展及协调器未实现，D-03/D-04开放。

2026-10-13补齐[公钥快照JSON加载与独立期望](architecture/GRANT_TOKEN_PROFILE.md#8-严格json加载与独立部署期望)，新增Schema、Go/Python版本大整数金标及LoadVerifier；Go3项/Python4项检查覆盖闭集字段、规范Base64、精确时间和期望错配。配置来源、高水位、防回滚/工厂分发未验，D-03/D-04开放。见[执行证据](architecture/evidence/key-snapshot-load-review.json)。

2026-10-13新增[不可变公钥Verifier快照](architecture/GRANT_TOKEN_PROFILE.md#7-不可变公钥快照与窗口)，复制公钥、记录版本摘要，验证快照/key签发与验证窗口；4项新增真实SDK测试覆盖重叠、截止、输入修改隔离和替换移除。没有工厂装配、可信分发/防回滚、刷新或当前授权，TOKEN-04局部推进、D-03开放。见[执行证据](architecture/evidence/grant-key-snapshot-review.json)。

2026-10-13接入[成熟Grant SDK适配器](architecture/GRANT_TOKEN_PROFILE.md#6-首个sdk适配器及局部运行证据)：go-jose/v4 v4.1.5与sum已锁定，4项Go真实签发/验签场景验证篡改、错绑定/header/key、算法混淆、精确时间和非法字段。成功只证明签名及请求绑定；Trust/Expected仍为合成输入，服务身份/当前授权、完整轮换与账本未验，D-03开放。见[执行证据](architecture/evidence/grant-sdk-runtime-review.json)。

2026-10-13补齐[Grant令牌及成熟SDK选型](architecture/GRANT_TOKEN_PROFILE.md)，固定go-jose/v4 v4.1.5源码/许可/发布证据，决定紧凑JWS、专用typ、Ed25519及受控公钥快照，规定精确时间和6项运行门槛。尚未安装SDK或验签，D-03/授权接入仍待验。

2026-10-13新增[17操作动作策略](architecture/REMOTE_OPERATION_POLICY.md)，金标scope.action按受控表修正；4项专项检查覆盖读写/控制分离及有界范围。当前主体、父任务/资源、验签和实时授权仍待验，authorized固定false，D-03开放。见[执行证据](architecture/evidence/operation-policy-review.json)。

2026-10-13扩展[全17操作Grant摘要对照](architecture/ACCESS_GRANT.md#6-全17操作局部摘要对照)：独立Schema及实际Go Decode入口检查每项请求/Scope金标，input变异改变摘要且解码拒绝。Go一项17子场景、Python两项检查；共享Scope动作仍为字段样例，实际operation权限映射/验签/服务身份和账本未验，D-03开放。见[执行证据](architecture/evidence/operation-grant-review.json)。

2026-10-13补齐[Grant摘要Go/Python金标](architecture/ACCESS_GRANT.md#5-gopython摘要金标)：6组规范字节/摘要一致，含实际解析请求和完整Scope，Go/Python各2项测试覆盖域/整数/Unicode与拒绝边界。只证明所选样例编码一致；17操作绑定、成熟验签、当前身份/权限及账本仍待验，D-03开放。见[执行证据](architecture/evidence/grant-digest-review.json)。

2026-10-13新增[远程Grant绑定契约](architecture/ACCESS_GRANT.md)、内部Bindings Schema及离线摘要/绑定检查，5项合成测试覆盖11项调用关系、精确时间和规范摘要。报告固定authorized=false；未选择token/成熟验签SDK，真实身份/权限、账本与跨语言摘要仍待验证，D-03开放。见[执行证据](architecture/evidence/access-grant-review.json)。

2026-10-13新增[封存投影包完整性](architecture/RETRIEVAL_EVALUATION.md#6-封存投影包完整性)，用平台规范编码分别绑定语料清单与问题/主体/快照/标注，读取真实本地UTF-8文件核对摘要、码点范围及库存；8项专项测试。正式原件/映射包、来源/人工标注、不可变资产及真实后端仍待验，阶段F未通过。见[执行证据](architecture/evidence/retrieval-seal-review.json)。

2026-10-13补齐[评测机器Schema与请求绑定](architecture/RETRIEVAL_EVALUATION.md#5-机器schema及请求绑定)，要求同题的问题文本/主体/权限和发布快照一致，闭集字段拒绝供应商载荷；新增5项检查及原7项评分检查通过，全量Python回归115项通过。真实封存来源、调用轨迹、输出映射和90%目标仍未验。见[执行证据](architecture/evidence/retrieval-schema-review.json)。

2026-10-13补齐[证据召回评测契约](architecture/RETRIEVAL_EVALUATION.md)和离线评分器：按版本/Block区间覆盖计算宏平均，错误/超时不删题，权限负例和泄露另计。7项合成测试通过；没有真实中文封存集或检索/质量/性能证据，报告固定正式验收未通过，阶段F待验。

2026-10-13新增[Qdrant独立向量适配设计](architecture/QDRANT_ADAPTER.md)，固定v1.16.2 commit、LICENSE及版本OpenAPI，明确vector端口、获准组合过滤、attempt隔离、标准内容重建和6项运行对照。源码/API入口不证明真实过滤或替换，关键词/嵌入/重排仍需配套，D-05开放。

2026-10-13新增[解析部署与替换边界](architecture/PARSER_DEPLOYMENT.md)，固定核对DocReader的22项直接声明、80项锁条目及系统工具链，明确OCR/VLM独立能力、字体/模型资产、子进程预算和6项双实现运行门槛。未安装/执行解析，完整许可、镜像身份和路由适配仍待验；D-03/D-05开放。

2026-10-13补齐[候选组件准入与部署边界](architecture/COMPONENT_ADMISSION.md)。WeKnora固定commit的LICENSE/Compose/go.mod/第三方声明及Dockerfile摘要已核验，明确ParaDB默认镜像、latest制品锁定和平台/引擎数据隔离。仅源码及官方发布元数据证据；完整依赖许可、制品摘要、部署和替换测试仍待完成，D-05开放。

2026-10-13补齐[上传状态转换规则](architecture/BLOB_CONFIRMATION.md#7-状态转换与fencing高水位)：保留历史fencing高水位，验证版本递增、候选/attempt固定、取消意图单调及未知结果恢复。新增9项转换测试；facts仍为合成输入，实际CAS、事实来源、存储和恢复待验，D-03/D-04保持开放。见[执行证据](architecture/evidence/upload-transition-review.json)。

2026-10-13新增[UploadOperation机器记录](architecture/BLOB_CONFIRMATION.md#6-机器记录与离线核对)与独立期望核对，7项合成检查覆盖八状态、精确整数、候选/结果错配、未知提交和封闭错误。Schema生成一致性及94项Python回归通过。报告固定不具备确认资格；真实状态转换/CAS、摘要重算、存储桥接、权限、事务及恢复仍待完成，D-03/D-04开放。见[执行证据](architecture/evidence/upload-operation-review.json)。

2026-10-13补充[上传确认与恢复协议](architecture/BLOB_CONFIRMATION.md)，明确接收、对象持久及元数据确认边界，UploadOperation身份/阶段、幂等摘要、提交未知探查、保留清理及9项双实现运行门槛。此次为架构文档交付，尚无确认协调器、持久仓储、暂存桥接或恢复运行证据，D-03/D-04继续开放。

2026-10-13新增[multipart上传与隔离暂存](architecture/BLOB_MULTIPART_TRANSPORT.md)：新增7项Go测试，覆盖5MiB流式写入、长度/摘要/结构/预算拒绝、前后模拟访问回调、失败清理及真实HTTP中断和服务器读取deadline。只返回验证receipt，不确认BlobRef；认证/Grant、存储提交、账本和恢复仍待实现，D-03保持开放。复核结果见[执行证据](architecture/evidence/blob-multipart-review.json)。

2026-10-13新增[有界SSE接收与本地HTTP验证](architecture/SSE_TRANSPORT.md)，接收真实响应Body，覆盖三类换行/BOM/多行data、回显/类型/预算、逐批闸门、终态延迟至正常EOF和取消关闭。8项Go场景含3项真实localhost HTTP测试（分块、模拟撤权、阻塞取消/sink关闭和静默peer超时）；Go全包测试/go vet与87项Python回归通过，见[执行证据](architecture/evidence/sse-transport-review.json)。访问闸门仍是测试回调，未接入真实授权/Grant、账本或模型；不能把网络读取结束当作后台任务停止。D-03继续开放。

2026-10-13目标已恢复为active，继续完成暂停前的控制接口草稿。D-03补齐[状态/取消映射](architecture/CONTROL_WIRE_MAPPING.md)：原/控制请求身份区分、六种状态、15类封闭结果及两种取消响应；22项实际Go样本和7类非法变异通过，复合search也完成共享检索DTO对照。Go全包测试/go vet、87项Python回归及全部8个映射脚本通过，见[执行证据](architecture/evidence/control-wire-review.json)和[17操作覆盖表](architecture/REMOTE_MAPPING_COVERAGE.md)。逐操作局部数据样本已齐，但HTTP/multipart/SSE、当前授权/Grant、账本、真实取消和操作级全量领域验证仍未通过，D-03继续开放。

D-03补齐[生成请求与回答事件映射](architecture/GENERATION_WIRE_MAPPING.md)：实际Go请求/预算、五类联合事件、Trace/timings及顺序检查；核对独立引用白名单/配置版本、完整增量正文与唯一终态，error不会成为成功回答。三种事件流样本、全部5类事件、7项非法变异和独立Schema对照通过；Go全包测试、受影响包新增测试、go vet与87项Python回归通过，见[执行证据](architecture/evidence/generation-wire-review.json)。仍没有模型、SSE帧/网络、当前授权、实时deadline/token计量或sink取消；control和完整远程协议验收未闭合。

D-03补齐[远程安全错误映射](architecture/ERROR_WIRE_MAPPING.md)：实际Go编码/解码检查内外请求关联，15种Schema错误码使用封闭安全模板，丢弃原始文案并限制重试建议。完整Code枚举的独立Schema前后检查、非法cause/code/ID拒绝、Go全包测试/go vet及87项Python回归通过，见[执行证据](architecture/evidence/error-wire-review.json)。供应商分类、调用账本、HTTP/重试策略、生成/控制及真实协议验证仍未完成。统一契约已纠正V1不存在details字段的旧描述。

D-03补齐[Blob远程映射](architecture/BLOB_WIRE_MAPPING.md)：三项put/open/delete的实际Go metadata编解码、关联ID及上传期望检查，另有EOF/长度/SHA-256/字节预算/context边界和响应头检查。独立Schema前后往返与3项非法样本拒绝通过，Go全包测试/go vet及87项Python回归通过，见[执行证据](architecture/evidence/blob-wire-review.json)。没有multipart/HTTP适配器、阻塞读取取消、Grant/当前授权或实际存储调用；生成/控制/安全错误映射及真实协议验收仍待完成，D-03未关闭。

D-04新增[原件复制与映射契约](architecture/BLOB_COPY.md)、Blob复制清单Schema及只读流式文件校验。11项专项测试使用真实临时文件，覆盖8MiB读取、篡改/截断、身份/space/版本及独立库存；87项Python回归和复制/KB状态/导出Schema一致性通过，见[执行证据](architecture/evidence/blob-copy-review.json)。未执行实际复制或存储适配器调用，不证明目标绑定不可变、原子映射、重启或回退；文件/数据库状态和typed记录契约仍待补齐。

D-04补齐[KB后端迁移状态机](architecture/MIGRATION_STATE.md)和记录Schema，区分切换、重启核对、恢复写入、取消/回退及保留窗口。13项专项测试、76项Python回归和迁移/日志/导出Schema生成一致性通过，见[执行证据](architecture/evidence/migration-state-review.json)。输入facts仍是离线合成观察值；真实CAS/租约接管/授权/切换、数据库与文件迁移状态、typed载荷及流桥接尚未完成，D-04保持开放。

D-04补齐[迁移日志与水位协议](architecture/JOURNAL_PROTOCOL.md)、完整事务日志包Schema和离线前驱链校验。明确V1单JournalHead提交屏障、独立快照/Head范围核对、完整事务payload与原子重放receipt；11项专项测试、63项Python回归和两份Schema生成一致性检查通过，见[执行证据](architecture/evidence/journal-contract-review.json)。证据只覆盖合成链，不证明真实提交调度、可信Head、目标数据库重放或切换；typed记录payload、迁移状态机器契约及正式运行验收仍未完成。

D-04新增[元数据导出契约](architecture/EXPORT_CONTRACT.md)、ExportManifest生成Schema与离线清单/记录包络校验，明确权威记录族、完整事务快照、租约失效和隔离导入闸门。9项专项测试、52项Python回归、生成一致性及部署结构检查通过，见[执行证据](architecture/evidence/export-manifest-review.json)。各类payload Schema、原件复制包、认证凭据安全迁移、真实提交水位/导入事务及正式部署核对仍待完成，D-04保持开放。

D-04进一步补齐[受控构建锁](architecture/BUILD_LOCK.md)、锁Schema和本地离线检查命令，关联部署/清单/制品/保留策略摘要，拒绝覆盖不全、版本失配和检查期间文件变化。10项专项测试及43项Python回归通过，见[执行证据](architecture/evidence/build-lock-review.json)。正式运行制品身份、来源信任、持久化epoch核对、导出包及迁移水位仍未完成，D-04未关闭。

D-04保留与回收契约已补齐策略Schema和离线期限关系校验：回退/重放/恢复窗口、幂等结果、孤立产物保护和64位溢出共7项测试通过，全量Python契约回归33项通过。权威保留锁、生产GC、BuildLock、ExportManifest、迁移水位机器契约和正式部署对照仍待实现/验证，D-04尚未关闭。详见[保留与回收契约](architecture/RETENTION_CONTRACT.md)及[复核证据](architecture/evidence/retention-contract-review.json)。

按用户最新要求，当前完善架构文档及架构契约验证，业务服务尚未启动。文档入口见 [设计索引](README.md)。设计完成、内核实现和生产验证分别记录。

2026-10-12已执行 [架构设计验收](architecture/ACCEPTANCE_REVIEW.md)：总体架构评审有条件通过，13项现有检查全部通过；复合能力映射、冻结哈希精确编码、远程模块机器契约尚未闭合，V1契约冻结与原方案完整运行验收未通过。报告列出逐项证据和关闭标准。

同日复核已补齐复合端口、内核能力映射及整组配置规则：两个Go内核试验与5项Python复合场景通过；允许独立扩展通道。D-01设计缺口已收敛，D-02/D-03与正式部署/真实适配验收仍待完成。

后续复核补齐D-02精确冻结编码、Schema、两级哈希及HashFormat。7项内容金标由Go/Python交叉核对，6项Python测试验证差异/非法输入；管理Revision响应同步格式、块哈希与附件清单。实际提交映射/事务、大载荷流式处理和D-03远程契约仍待完成，不能把哈希辅助包视为完整知识治理。

D-03新增17项远程模块OpenAPI与传输规则，7项结构/日期/有限数值测试和规范检查通过。完整字段映射/往返样例、服务认证/Grant/当前授权验证、调用账本和实际取消/网络联调仍待完成，D-03尚未关闭。

解析端口进一步补齐实际Go/wire编解码，7组金标通过独立Schema及规范字节往返；请求/响应关联、完整内容字段、大整数/纳秒时间和非法输入有证据。其他端口映射、完整Schema/领域验证、真实解析及授权/网络仍未完成，D-03未关闭。

嵌入/检索/重排新增实际Go/wire映射与三个独立Schema前后对照；Generation补齐状态/绑定/证明，结果校验规格、数量、维度及本次目标来源。索引/文件/生成/控制映射、认证/Grant与网络验收仍待完成，不能把字段过滤检查当成真实后端过滤证明。

索引/复合prepare/verify/delete六项操作新增Go/wire对照与独立Schema往返，准备证明按独立权威期望核验；删除使用显式mode。映射/租约/真实Verify/发布与删除业务未执行；文件/生成/控制和安全/网络门槛仍待完成，D-03未关闭。

### 架构设计交付

- [x] 模块边界、分层依赖、数据所有权和部署设计。
- [x] 标准对象、端口语义、错误、事件和配置兼容规则。
- [x] 发布 CAS、索引代际、撤回、幂等和任务恢复设计。
- [x] REST/MCP 四项伙伴能力与身份传递设计。
- [x] 架构决策、候选组件矩阵和待验证能力清单。
- [x] 替换、迁移回退、故障场景与召回/性能验收方案。
- [x] 一致性审查：文档/知识库代际分离、权威租约、尝试级索引隔离、迁移日志、撤销时序和修订引用。
- [x] 模块清单/部署配置/索引与查询策略 JSON Schema、装配语义、生命周期与结构验证样例；运行注册中心与完整语义校验仍待实现。
- [x] 逻辑数据模型、草稿冻结、记录约束、事务矩阵、删除回收和仓储替换门槛。
- [x] 管理界面/命令契约与原始20模块、五类前期成果、最终验收条件的证据审查。
- [x] WeKnora固定commit/API核验：文本写入与分块接口，检索投影适配方案及试验门槛；未部署运行。
- [x] 伙伴及管理OpenAPI草案、表格/图片/音频类型化载荷与DTO/流事件样例；语义校验和正式协议联调仍待完成。
- [x] 管理API的26项操作、审核队列、冻结版本读取及契约生成/结构样例；端点实现和管理界面仍待完成。
- [x] 伙伴/管理OpenAPI3.1.1规范校验、必需字段拒绝对照、文件哈希报告与校验环境版本记录；不代表运行端点已通过。
- [x] MCP协议/Go SDK候选固定源码核验、传输认证、四工具映射与互操作方案；未集成SDK或运行联调。
- [x] 从伙伴OpenAPI生成四项MCP自包含工具契约与过期检查；运行服务的schema/isError/身份行为仍待实现验证。
- [x] 显式多路索引/召回配置、通道覆盖、代际兼容和全局候选预算设计；路线语义校验与运行替换仍待实现。
- [x] 假设模块的离线装配语义样例（能力、版本、依赖、通道与预算）；真实注册中心和后端能力验证仍待实现。
- [x] 复合组跨Pipeline绑定的完整性检查与拆分拒绝样例；后端内部耦合和实际整组迁移仍待验证。
- [x] 首批10项Go标准端口与核心/内容/检索模型，独立编译与go vet通过，仅依赖context/io/time；尚无能力实现。
- [x] 主要能力及辅助仓储Go草案共29个接口，补齐接入/审核/生成/反思/记忆/图谱/任务/事务/反馈/评测观测；单包编译与静态检查通过，缺行为实现和传输映射。
- [x] Go配置/代际/查询快照、审批请求记录与命令分离、多通道准备证明及回答追踪字段对照；不变量与映射仍待行为测试。
- [x] 代际准备关系校验器及复合/独立通道证明测试，拒绝16类不一致目标；真实后端Verify、权限、审批、栅栏与发布CAS仍待实现。
- [x] 文件级离线配置检查入口：读取正式部署格式及候选清单，输出文件哈希、依赖顺序与未验证门槛；8项输入/Schema/引用/信息隔离测试通过。正式部署归一化和运行就绪仍待实现。

以上表示设计已成文，不表示真实适配器已验证。WeKnora 标准内容往返、有效版本过滤规模、中文全文引擎与模型选择仍需试验收敛。

### 实现与验证待办

- [x] 架构方案归档到 docs。
- [ ] 标准数据模型/主要端口草案已成文；完成全量字段、状态机、边界验证及传输映射的契约验收。
- [ ] 模块清单、注册中心、依赖和契约版本检查。
- [ ] 配置加载、不可变配置版本、启动健康检查和资源回收。
- [ ] 两种解析器与两种文件存储实现的公共契约测试。
- [x] 可运行的离线配置与清单检查命令；生产启动检查命令仍待实现。
- [ ] 将架构契约生成 Go 类型、JSON Schema、OpenAPI，并运行适配器验证方案。
- [ ] 自动化测试、静态检查和持续集成。

## 后续阶段：保持完整目标

- [ ] 空间/用户/知识库权限及凭据管理。
- [ ] 不可变知识版本、人工审核、原子发布与撤回。
- [ ] 多格式解析生产适配器及云端嵌入/重排/生成模型。
- [ ] PostgreSQL/全文/向量索引、Redis 任务与持久化事件。
- [ ] API 与 MCP 共用知识应用服务及伙伴联调。
- [ ] Vue 知识管理界面。
- [ ] 检索反思、反馈草稿及降级处理。
- [ ] 模块迁移、索引代际、数据导出导入与回退。
- [ ] 长期记忆与图谱的可替换实现。
- [ ] 独立中文评测集与 Recall@10 ≥90% 验收。
- [ ] 固定负载下的响应性能、部署与恢复验收。

以上未完成项不会通过测试替身、示例数据或接口声明替代。当前没有真实行业召回率或生产负载测试结果。

内部模块装配内核已实现并通过包测试：受控工厂目录、强类型端口、能力/版本/特性检查、依赖槽与拓扑、启动健康检查、失败回收和只读快照。配置读取拒绝未知字段、重复JSON键、非对象根、尾随数据及大小/嵌套超限。细节和生产接入边界见 [内核设计](architecture/KERNEL_RUNTIME.md)。上方涉及“真实注册中心待实现”的条目指完整正式部署接入及真实适配器验收，不能与内部内核混同。

2026-10-12执行完整 `go test -cover ./...` 和 `go vet ./...` 通过；contracts语句覆盖率92.6%，kernel为89.6%。这些测试覆盖现有关系校验器和假设模块装配，不证明生产解析、权限、发布、检索、迁移或召回目标通过。正式部署Schema到内部计划的归一化、制品锁、流水线绑定与应用入口仍待实现。


2026-10-15：已实现[原生草稿冻结映射](architecture/NATIVE_FROZEN_MAPPING.md)，文本/表格/图片/音频按平台规范生成独立载荷及双摘要，通过7组独立Schema/Python金标。数据库草稿也接入同一映射，当前PG15/16/18检查增至55项。提交审核、不可变Revision持久化与审批事务仍待实施，完整知识治理闭环未通过。


2026-10-15最新进展：已实现[草稿提交审核事务](architecture/SUBMISSION_TRANSACTION.md)，原件验证在写事务外执行，事务内校验版本与载荷摘要。冻结Revision、pending审核请求、submitted草稿、回执、审计与outbox共同提交；配置绑定、同键冷连接重放、撤权、回滚、不可变保护和并发单版本均有运行证据。PG15.19/16.15/18.6使用相同425项输入及同一探针二进制，各通过64项检查；Go测试/vet、227项Python测试、OpenAPI及7组冻结金标通过。证据见[提交验收记录](architecture/evidence/submission-transaction-review.json)。

下一步仍是审批决定、有效批准与发布链路，并补齐真实IAM、完整UoW/迁移日志和正式应用入口；这些缺口不由本次局部提交验收替代。


2026-10-15最新进展：已实现[审批决定事务](architecture/REVIEW_TRANSACTION.md)，包含批准/拒绝/撤销、不可变决定、CAS当前指针、幂等历史回执及审计/outbox。复核并修正上一轮初始待审契约，统一为submitted/版本0。PG15.19/16.15/18.6使用相同431项输入及同一探针二进制，各通过73项仓储/应用检查；21个真实审批投影通过管理OpenAPI校验。Go全包测试/vet、227项Python测试及7组冻结金标通过，实例与临时目录均已清理。

下一步是当前有效批准、发布前检查及原子发布/检索接入；真实IAM、正式管理接口、完整UoW/迁移日志和召回性能目标仍未通过。


2026-10-15最新进展：已实现[当前有效批准与发布准备登记](architecture/PUBLICATION_START.md)。当前规则头、审批指针和后端头在事务内受写入屏障保护；操作、初始任务、审计及outbox共同提交，支持冷连接重放、政策/后端切换、撤权、回滚与同键并发。PG15.19/16.15/18.6使用相同437项输入及同一探针，各通过83项检查；18个发布操作/任务投影与9个规则摘要通过独立Schema/跨语言对照，审批21个投影及冻结7组金标保持通过。Go全包测试/vet、227项Python回归与OpenAPI通过，私有实例均已清理。

下一步为发布任务的租约/恢复、实际索引准备、ReadinessProof及最终发布指针原子切换。当前只有preparing登记和queued初始任务，不代表已完成发布或已发布检索。真实权限、完整UoW/迁移治理、REST/MCP/UI与Recall目标仍开放。


2026-10-15最新进展：已实现[发布准备租约与内容读取](architecture/PUBLICATION_TASKS.md)的Claim/Renew/ReadOwned，分离稳定CommandID与调用追踪ID，支持提交后丢失响应的冷连接恢复、到期接管、更高fence及精确租约核验。Worker与原发布者分别再授权，配置ABA/批准撤销/epoch变化导致过期计划隔离；回执与任务头不一致会整体回滚。PG15.19/16.15/18.6使用相同443项输入及同一Go探针，各通过96项仓储/应用检查；21个任务投影、12份租约样本及已有发布/审批/冻结对照通过。统一Go测试/vet、227项Python回归、OpenAPI通过，私有实例/目录/端口已清理并核对。

下一步接入实际索引准备与外部写入fencing，持久化并独立验证ReadinessProof，再完成任务与最终发布CAS。当前没有持续队列、完整TaskRepository/迁移、正式API/MCP/UI或Recall实测成果。

2026-10-15最新进展：已实现[实际关键词索引准备与独立读回](architecture/INDEX_PREPARATION.md)。Worker使用独立任务/分块/配置/后端端口；默认PG复合适配器以同事务租约锁保护实际投影、attempt独立代际与产物/审计/outbox，另开事务读回验证。相同448项输入与同一探针在PG15.19/16.15/18.6各通过108项检查；6组实际产物/快照及标准准备DTO、已有任务/发布登记/审批/冻结对照保持通过。Go测试/vet、227项Python回归与三份OpenAPI通过，实例/目录/端口已清理核对。

下一步为任务完成、未知结果恢复和最终发布CAS，再贯通有效检索。当前仅文本关键词准备，不冒充完整IndexWriter或第二后端替换；持续队列、外部引擎写入隔离、真实IAM、完整UoW/迁移、REST/MCP/UI与Recall/性能验收仍开放。

2026-10-15最新进展：已实现[准备任务完成/未知结果恢复](architecture/PREPARATION_COMPLETION.md)。独立CompletionStore仅依赖平台类型，默认PG复合实现重新核对真实投影、当前控制事实与精确租约后原子保存completed、不可变回执、审计/outbox；过期执行租约不阻止获准主体恢复已提交历史响应。相同453项输入和同一探针在PG15/16/18各通过122项检查；6组完成响应/Job投影通过独立关系与Schema验证。全Go测试/vet、227项Python及OpenAPI通过，私有目录和端口已独立确认清理。

下一步是最终发布CAS与有效检索；完整队列/TaskRepository、真实权限、第二准备后端、全量迁移回退、产品入口及Recall目标仍开放。公共任务终态沿用completed，独立Schema发现过状态名称不一致并已修复、重跑验证。

2026-10-15最新进展：已实现[最终发布与替换事务](architecture/FINAL_PUBLICATION.md)。PublicationStore仅使用平台对象，默认PG复合适配器实时复核已完成任务/真实索引/当前批准配置，CAS与历史、旧源清理、稳定回执、审计/outbox共同提交。实测首次发布、同Revision重建替换、旧清理隔离、正常撤回和撤回后历史重放。相同458项输入与同一探针在PG15/16/18各通过138项检查；6份实际结果、12个公共投影及6个命令摘要通过独立关系/Schema/跨语言核验，Go/vet、227项Python与OpenAPI通过，测试资源清理已核对。

下一步是有效发布检索与当前证据复核。当前仍不是完整生产UoW/服务：新正文换版、真实IAM、外部引擎/第二后端、完整队列清理、全量迁移回退、产品API/MCP/UI及Recall/性能目标保持开放。


2026-10-15最新进展：已实现[PostgreSQL查询权威读层](architecture/POSTGRES_QUERY_READS.md)，上层使用标准QueryReadUnit/Repository，同一只读Repeatable Read读取有效发布、当前批准/规则及后端代际，分页游标和句柄限制在回调内。真实撤回、撤销批准、对象隔离、权限回调、取消和store epoch轮换均有实库场景；撤回前的当前元数据比对通过，新读取后明确拒绝旧来源。同一461项输入与同一Go探针在PG15.19/16.15/18.6各通过150项累计检查，三组真实观察投影通过独立关系校验；Go全量测试/vet、227项Python与三份OpenAPI通过，测试实例/目录/端口清理已核对。

下一步贯通正式Retriever、真实索引及正文证据核验与输出前复核。当前实现仅证明元数据读取子集，最多1000个有效发布头；真实IAM、生产装配、第二仓储替换/恢复、REST/MCP/UI与Recall/性能验收继续开放。


2026-10-15最新进展：已实现[当前发布关键词Retriever](architecture/POSTGRES_KEYWORD_RETRIEVER.md)，通过标准Retriever接口调用，可信发布投影与实际当前发布/批准/代际比对后，在同一主库快照核验真实索引并执行SQL查询，返回前重新授权并另开观察点复核。实际准备投影保留Published=false，检索依据平台当前发布头；未发布、撤回、撤销批准或实际索引删除均会拒绝旧请求。独立对照三版PG共9个实际候选、18条原生索引记录、3组发布投影摘要及模块Schema。相同464项输入与同一Go探针在PG15.19/16.15/18.6各通过164项检查；Go全量测试/vet、227项Python及三份OpenAPI通过，测试资源清理已独立核对。

下一步接最终正文/索引证据提供者与当前输出复核，再贯通REST/MCP和产品入口。当前只有关键词候选检索；真实身份及QueryAuthority/Grant、更多召回通道、第二引擎替换、生产规模延迟、中文Recall@10≥90%、迁移回退均保持开放。


2026-10-15最新进展：已实现[当前发布冻结正文读取器](architecture/PUBLISHED_FROZEN_READER.md)，通过标准FrozenContentReader端口读取真实持久化正文。当前发布/批准准入后，以类型化Revision重新构建标准JSON并逐字节及双摘要比对，返回前新观察点复核；与发布准备任务共享底层内容核验，并保留操作/审核文档及版本一致性检查。三版PG共9份实际正文通过独立Schema/canonical/双摘要及归属验证。相同466项输入与同一探针在PG15.19/16.15/18.6各通过172项检查，Go全量测试/vet、227项Python与三份OpenAPI通过，私有测试资源清理已独立核对。

下一步补具体引用位置与原件字节核验，再接完整CurrentBackend/CurrentControl及产品输出。现有原生引用为original/coarse，既有文本核验器仅支持revision定位；不能改写来源类型来跳过校验。真实IAM、混合召回、第二引擎替换、生产迁移/回退、REST/MCP/UI及Recall/性能继续开放。


本轮进展：新增[原件字节核验](architecture/ORIGINAL_BYTES_VERIFICATION.md)，通过标准BlobStore读取完整流并验证长度、SHA-256、EOF及成功关闭，保留当前Scope/Action，不自动授予原件读取权限。真实本地存储和拒权、截断/超长/错误摘要、读取/关闭错误、无进展与取消检查通过；当前468项输入的Go全量测试/vet、227项Python及三份OpenAPI均通过。

下一步从已核实的冻结正文绑定BlobRef，并串联来源锚点、摘录/定位与输出前权限复核。本轮没有新增PostgreSQL集成证据；原三版各172项保留为466项输入的历史基线，不能代替新模块或完整输出验收。


本轮进展：已实现[批准正文与原件读取绑定](architecture/APPROVED_ORIGINAL_BINDING.md)，只从新读取且双摘要通过的冻结正文清单提取完整BlobRef；经独立原件控制源解析读取授权，实际核验文件后再次核对授权与批准正文。最终正文检查受最新文件授权期限约束，授权已过期或在检查中到期均失败。修复AKOS-CJSON整数指数表示兼容，并用规范字节往返比较拒绝小数舍入、null及超限大小。真实本地文件与测试正文/控制提供者的正反向检查通过；当前470项输入的Go全量测试/vet、227项Python及三份OpenAPI通过。

下一步把具体Evidence绑定到批准块和原件来源锚点，按格式核验摘录及位置，再接CurrentBackend和产品输出。生产控制源与本轮PostgreSQL联调仍未完成；原PG三版各172项保留为466项输入历史证据，不能用于宣称本轮完整输出验收。


本轮进展：已实现[文本原件粗粒度引用核验](architecture/ORIGINAL_TEXT_EVIDENCE.md)。Evidence必须匹配指定批准文本块与完整original/coarse锚点，摘录既存在于批准块，也存在于实际核实原件字节的文本投影。只捕获同一已验流交给可注入的文本投影器，投影受原件读取期限限制，完成后再次检查原件和批准状态。默认UTF-8文本/Markdown已通过真实文件正向及错误绑定、损坏、期间批准失效等检查；人工新增文字不能冒充原件引文。当前472项输入的Go全量测试/vet、227项Python及三份OpenAPI通过。

下一步接真实CurrentBackend及PostgreSQL输出复核，并接入PDF/Office等成熟投影组件。页码、表格、OCR与其他精确定位当前明确拒绝；进程内投影端口不代表远程模块/替换验收。生产权限、最终模型/API输出、Recall/性能、迁移回退继续开放；旧PG172项仍为466项输入历史基线。


2026-10-16最新进展：已实现[PostgreSQL当前证据后端](architecture/POSTGRES_EVIDENCE_BACKEND.md)，由CheckCurrent实际串联当前发布/审核、真实关键词索引、类型化冻结正文及本地原件摘录。释放SQL读事务后处理原件，再开新观察点重核实际索引和发布；实库覆盖投影期间删除索引、原件独立拒权、最终控制撤权、人工修订不冒充原件，以及撤回/撤销批准。实库复现并修复分块逻辑删除但物理行仍在时旧核验可通过的问题，检索与证据后端现在均拒绝相关旧代际。相同475项输入与同一Go探针在PG15.19/16.15/18.6各通过183项检查；3组完整实际CheckCurrent、12份冻结正文、3个关键词候选和51条样本索引记录的独立复核通过。Go全量测试/vet、227项Python与三份OpenAPI通过；PG测试目录/端口清理已独立核对。

下一步接生产身份/CurrentControl与统一知识应用服务、模型及REST/MCP输出，扩展PDF/Office等来源投影。当前用户/原件授权与最终控制仍为fixture，模块部署装配、精确定位、第二后端替换、完整迁移回退、Recall@10≥90%和固定条件性能仍待验收。局部实库接入不关闭完整CHECK或总体验收。


2026-10-16最新进展：已将[PDF/Office原件文本投影](architecture/OFFICE_ORIGINAL_PROJECTION.md)接入现有受控解析工厂。新增可选original.text及original.format特性，按已准入MIME路由成熟转换器，沿用字节限制、健康与关闭状态。Word/Excel/PowerPoint及普通英文PDF通过真实文件上传、解析、冻结正文/原件引用及虚构摘录拒绝检查；三份PDF×MarkItDown/PyPDF验证同一标准调用与关闭后引用拒绝。当前477项输入的Go全量测试/vet、228项Python和三份OpenAPI通过；三版PG各183项及独立运行复核同步通过，测试资源清理已核对。

这是parser实例的复合能力，未完成独立投影远程协议/部署绑定。Office/PDF样本使用测试批准正文/权限，尚未接数据库审核发布和最终模型/API输出；PG证据仍为UTF-8原件。中文复杂/扫描PDF、OCR、精确位置/表格语义、生产身份/CurrentControl、Recall与性能、迁移回退继续开放。


2026-10-16最新进展：新增[AuthenticatedControl](architecture/AUTHENTICATED_CURRENT_CONTROL.md)，绑定已准备操作并通过标准Authenticator、Authorizer及CurrentFactsSource逐次重新认证、授权和读取当前配置/审核规则；不复用旧Principal作为认证结果。凭据副本、认证后清除、关闭后拒绝、日志/JSON脱敏、范围/资源/请求绑定和提供者错误隔离已验证；后端期间撤销凭据会在最终重新认证时阻止继续授权。实库CheckCurrent正向链路已使用该编排，认证/授权提供者仍为fixture，规则/索引/正文/原件实际读取。

同一479项输入与同一探针在三版PG各通过184项检查，独立样本确认认证和授权各两次并保留fixture身份标记；Go全量测试/vet、228项Python、三份OpenAPI及成熟组件回归均通过，PG测试资源清理已核对。下一步接入实际身份与策略/配置来源及统一应用服务；生产IAM/RLS、Grant账本、模块部署、最终模型/API/MCP输出、Recall/性能与迁移回退继续开放。
2026-10-18当前稳定窗口更新：加入FactoryRegistry及Profile→kernel可替换工厂解析后的625项源码快照已通过PG16.15 WSL完整探针，临时实例停止与资源清理完成；fixture身份/部署仍使`production_acceptance=false`。见[当前PG证据](architecture/evidence/current-postgres16-wsl.json)。
