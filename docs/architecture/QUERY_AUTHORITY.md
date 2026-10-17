# 查询权威服务与可替换读仓储

本规范补齐[分库请求](PARTITION_REQUEST.md)和[发布投影](PUBLICATION_PROJECTION.md)的权威来源。现有MetadataRepository只有按ID读取Document/Revision/GenerationSet/BackendEpoch，没有一致地列举当前发布库存的接口；不能用这些独立读或直接查供应商索引来证明查询快照有效。当前已有设计及Go读端口声明/页面关系检查；真实服务与仓储尚未实现。

## 1. 服务归属与模块边界

QueryAuthority是自有知识应用中的必需协调服务，负责Prepare、PreparePartition和CheckCurrent三项操作。它组合Authenticator/Authorizer、只读仓储及已冻结配置，不交由WeKnora、Qdrant、OpenSearch、模型或记忆模块实现权威判断。

| 边界 | 职责与替换条件 |
|---|---|
| Authorizer | 依据已验证Principal、动作和目标返回当前Scope；独立替换须保持主体/父子范围/策略版本/失效语义 |
| QueryReadUnit | 提供固定一致性观察点的只读回调，不暴露数据库事务或连接类型 |
| QueryReadRepository | 在上述观察点内列举发布对象、有效epoch、版本与代际关系，返回平台事实模型 |
| QueryAuthority | 自有应用规则，产生固定快照及子投影，核验每个证据；仅依赖上述端口 |
| Retriever/CompositeEngine | 消费收窄请求，强制前置过滤；不产生权威发布或授权事实 |

这不是新增一个能随意跳过的可选检索路线。API/MCP、工作池和生成路径都调用同一QueryAuthority；它不可用时请求终止。未来拆成远程服务时，必须单独定义身份、版本化DTO、deadline及配置依赖后接入，不能直接把进程内句柄序列化。

## 2. 只读端口的语义契约

拟定QueryReadUnit.WithinRead(ctx, scope, callback)为回调提供QueryReadRepository及ReadIdentity。ReadIdentity包含store_epoch、read_version和watermark；它是实现提供的观察点身份，不是后端索引水位。回调结束后句柄失效，不能被后台任务继续使用。

QueryReadRepository的ReadPublishedPage读取给定Space/KB集合在该观察点的当前发布事实；页面包含Document ID/row_version/publication_epoch、唯一有效Revision、对应GenerationSet、有效BackendEpoch和批准版本引用。每项必须具备可验证的关系，不只给几个独立ID。删除、撤回、未批准版本不进入可检索库存，但其状态仍可被CheckCurrent查询。

分页游标不透明，绑定ReadIdentity、请求范围及稳定Document排序；所有页面必须在同一观察点读取。重复、跨空间、变更观察点或越过游标范围时明确失败。Schema/页面大小尚待定义；投影超过1000 entries或原始4MiB时返回明确的准入限制，不能截断成“完整快照”或偷偷放弃其他KB。

不能要求其他仓储实现伪造PostgreSQL事务ID。实现可采用真实只读快照，或经过共同测试的版本观察/重试机制；若无法提供契约要求的一致性，拒绝正式装配。分布式存储的异步副本不得只凭“已同步”声明满足它。

## 3. Prepare与PreparePartition

Prepare先验证Principal和当前父Scope，冻结Pipeline/QueryProfile，再在QueryReadUnit内读取有效发布库存、epoch与GenerationSet关系。构成父QuerySnapshot及相应事实身份后，再核对授权策略版本没有变化。冲突时有界重试整个准备；不能仅重读某一项后与旧页面拼接。

在同一权威数据库维护本地授权策略时，可以在共同观察点核验其版本。外部权限服务和元数据库没有天然的跨系统事务：严格模式必须有可核验的策略修订身份与本地接收门，或采用已经验证的一致性协议；无法核验时拒绝准备。前后读取版本一致不自动证明任意外部服务的线性一致性，必须通过具体适配器测试声明实际保证。

PreparePartition根据冻结Call取得目标KB的子授权，检查它不扩大父主体/动作/空间/范围且有效期不晚于父Scope；以父观察点事实建立独立投影。Invocation由应用分配，投影摘要由自有服务生成。子授权与父快照已发生变化时丢弃本次准备并重新开始，不回写不可变父快照或投影。

发布事实、配置兼容和完整Readiness检查通过后，应用构造RetrieveRequest并调用现有ValidateRequest；最后生成新Grant。QueryAuthority不持有检索引擎连接，检索服务替换不改变准备规则。标准字段核验与真实事实核验分别记录，不能用前者替代后者。

## 4. CheckCurrent与输出边界

CheckCurrent接收已验证Principal、准备快照身份及平台证据身份，重新核验当前策略、Document发布状态/版本、有效epoch/GenerationSet、来源Block及原件关系。后端返回的正文不能作为这些事实的唯一来源；平台对照权威内容和位置验证引用。

返回值只能是明确允许、拒绝或错误。拒绝/未知/仓储故障/策略服务故障都阻止输出，不按可选路线降级。必需检查在以下边界执行：向重排/生成/外部工具发送正文之前；返回检索证据/回答之前；流式每个对外批次之前。缓存和记忆召回同样执行。

检查之后并发撤回仍可能发生，必须明确可实现的线性化边界。首版保证“每次批次检查所观察的当前状态允许”；已发送的数据不能追回，不能承诺跨网络期间绝无撤回竞态。若业务要求撤回提交后所有未发送批次立即阻止，需共同锁/版本栅栏和发送协调协议，在运行验证前不得宣称已支持此更强保证。

epoch切换时不得把旧候选的GenerationID改成新代际继续返回。可按既定预算重新准备并检索，或返回快照过时；不得无提示扩大范围或复用旧Grant。权限/发布结果不缓存为永久允许，事件缓存只能加速明确失效，不能成为真实性来源。

## 5. 依赖、审计与故障

受控装配须声明权限与QueryReadUnit必需依赖，启动健康检查验证可读性及观察点能力。事务只读范围不可写发布、审批或授权状态；所有副作用仍走既有命令/UnitOfWork。

追踪记录包含父请求/子Invocation、Scope关系、配置版本、ReadIdentity、父摘要/子投影摘要、输入规模、检查阶段及静态失败代码。受保护的记录不含密钥或未经许可正文。追踪写入失败是否阻止请求由审计策略明确配置；强制审计场景必须失败关闭，不能默默丢失记录。

冲突重试只适用于可核验的一致性冲突，且计入30秒父预算；拒绝、未知授权、非法关系不自动重试。所有外部调用遵守取消，过期回调不能使用已经释放的读句柄。维护/恢复期间数据库store_epoch变化使旧ReadIdentity失效，不能复用恢复前快照。

## 6. 实施与验收门槛

| 门槛 | 验收证据 |
|---|---|
| AUTHORITY-01 读快照 | 真实仓储多页读取期间并发发布/撤回/切epoch，无混合观察点、遗漏或重复；只读句柄结束后不可用 |
| AUTHORITY-02 授权一致性 | 父子Scope来源、范围与有效期；本地及所选外部策略实现并发修订/故障明确失败，不以版本名字冒充保证 |
| AUTHORITY-03 准备贯通 | 权威事实→父快照→Call→子投影→实际RetrieveRequest→新Grant；结构核验及来源核验均有证据 |
| AUTHORITY-04 当前复核 | 检索中撤权/撤回/切epoch、缓存/记忆及流式批次检查拒绝旧证据，明确线性化边界 |
| AUTHORITY-05 故障与恢复 | 读超时、授权故障、审计故障、取消/句柄释放、store_epoch变化都按规则处理 |
| AUTHORITY-06 替换 | 两种读仓储或权限实现通过同一套上述测试；检索模块与伙伴接口保持兼容 |

先建立类型化读端口与固定数据模型，再实现一个真实仓储，通过并发验证后接入完整查询应用。已有投影Schema、摘要、Go关系检查和工作池不能证明这些门槛。当前仅完成服务设计，AUTHORITY门槛均未执行，PART/COORD与整体保持开放。

## 7. 首批Go读端口与页面关系检查

[query_read.go](../../pkg/contracts/query_read.go)新增QueryReadUnit、QueryReadRepository、QueryReadIdentity、PublishedFact/Page和CurrentDocumentState。WithinRead回调传入观察点身份及只读仓储；仓储提供ReadPublishedPage、LoadActiveEpoch、LoadCurrentDocument，不包含写方法或数据库专有类型。它们当前是声明，没有实际事务/句柄保护或模块工厂装配。

PublishedFact包含批准引用、内容摘要、Document版本及GenerationSet关系；页面Limit为1–1000，选定KB为1–256。ValidatePublishedPage核对给定观察点、Scope范围/空间、页面大小、相邻Document严格排序、同页Revision/Set/Generation唯一、版本/批准引用/内容摘要非空及代际KB/Revision/epoch/通道/state关系。调用方提供上一页末Document ID检查跨页顺序，不由游标字符串猜测。空终止页面允许，空页面却携带下一游标、游标原地循环拒绝。

这是关系校验，不能证明批准真实、发布仍有效、后端Ready、库存完整或opaque游标绑定正确。跨页代际/版本关系库存累计、页面预算、实际cursor绑定、当前Scope和ReadIdentity来源仍由仓储与应用完成。Current状态复核须另开新的WithinRead获取当前观察点，不能在旧父快照句柄中读一次便声称是当前状态。

4项Go检查覆盖合法/空终止页、观察点/空间/KB/重复排序、批准/版本/代际关系及游标循环/超额。全Go测试和vet通过，见[证据](evidence/query-read-ports-review.json)。真实AUTHORITY-01/02至06门槛尚未执行，接口声明与合成关系检查不作为完整运行验收。

## 8. 有界多页库存收集器

`pkg/queryread.Collect`在一个已建立的WithinRead回调中使用固定ReadIdentity与目标KB，逐页验证上述关系，并维护上一页末Document和跨页Revision/GenerationSet/Generation库存。重复或变更观察点、跨页排序倒退、opaque游标循环均失败，不将任何已收集部分返回。

部署Limits包括PageSize≤1000、MaxPages≤1000、MaxFacts≤10000、MaxGenerations≤30000，均为正数；它们是准备阶段总库存上限，与单个发布投影1000 entries的边界不同。每页请求Limit不超过剩余事实预算；还有下一游标但预算用尽时返回ErrLimit，不能截断为成功。游标字符串最多4096字节，收集器只检查循环/大小，不解析或宣称验证其签名/范围绑定。

要求有限父context，传递取消并在读取后/循环/返回前检查。权限范围结构不符时零读取；仓储错误静态ErrRead而非ErrUnavailable，不能进入可选检索降级。仓储必须按端口契约交付独立快照，收集器不深复制所有内部内容或证明其真实性；返回事实仍待Ready、有效epoch、批准和当前状态核验。

5项Go检查覆盖完整多页及固定请求身份、跨页重复/变更观察点、非原地游标循环、三类预算不截断、取消/静态读取错误/准入零调用。全Go测试及vet通过，见[证据](evidence/query-read-collection-review.json)。使用测试仓储，未实现真实WithinRead、一致观察点、cursor绑定或数据完整性证明；AUTHORITY-01/03仍待验。

## 9. 回调仓储生命周期包装

`pkg/queryread.NewGuard`在一个实际WithinRead观察点内捕获Scope和ReadIdentity，复制Scope KB列表，要求有限父context与未过期Scope，将调用deadline限制到父预算/Scope有效期。Guard实现QueryReadRepository，串行进入底层事务读，防止未经声明的并发事务操作；它不创建或认证数据库观察点。

主机必须在回调中defer Guard.Close并只交付包装句柄，不能同时把原始repository交给调用者。Close先取消观察点context，再等待持有锁的读取退出，设置关闭状态；之后包括保留旧引用在内的三类读方法都返回静态ErrClosed而不触达底层。取消/过期或底层错误不会返回部分结构。底层忽略context可使Close阻塞，合作取消与实际驱动资源释放仍需验证。

ReadPublishedPage核对固定观察点和Scope、字段关系，并避免底层改写请求KB切片影响期望。LoadActiveEpoch在调用前核验KB属于Scope；LoadCurrentDocument核对返回Document ID、空间/KB及版本，底层仍须先实施所有权过滤。包装器不证明授权来源、游标签名、Ready事实或当前发布，只管理给定句柄的范围/生命周期；仍依赖底层返回独立快照。

4项Go检查覆盖关闭引用/幂等Close、合作取消与活跃读取join、Scope副本/观察点错配、过期Scope及无父deadline拒绝。全Go回归和vet通过，见[证据](evidence/query-read-guard-review.json)。未实现真实WithinRead装配或真实事务释放，AUTHORITY-01/05保持开放。

## 10. ReadUnit自动保护与回调协议

`pkg/queryread.ProtectUnit`包装已准入的QueryReadUnit，在应用回调前自动创建Guard，仅交付受保护仓储；回调退出时自动Close，应用不再手写每次生命周期清理。Scope范围分别复制给底层与Guard，底层改写自己的列表不改变期望。主机不得另行暴露原始unit/repository，否则该包装不能阻止绕过。

准入要求有限父deadline、有效Scope和非空唯一KB范围；实际授权来源仍须外部核验。底层必须同步调用应用回调恰一次并等待其结束。遗漏/重复/提前异步返回均为ErrInvalid；回调错误即使底层吞掉也返回给应用，成功回调后的驱动错误转为静态ErrRead。观察点中的受保护调用分享父/Scope截止，结束时再核验context及实际deadline，避免父子context计时器传播先后造成过期成功。

违反同步协议时取消观察点并关闭已创建Guard；合作取消回调可退出。包装器不能强行停止忽略context的任意应用代码，也不证明坏适配器已释放数据库资源。读仓储原始句柄不泄漏、事务实际关闭、服务身份/当前策略及真正一致性仍属于真实适配器准入门槛。

7项Go检查覆盖自动包装与库存收集、失败后清理/回调错误保留、未调用/重复调用、静态驱动错误、Scope副本/非法准入、Scope在回调期间到期及异步提前返回。到期成功竞态在回归中重现并修复。全Go测试和vet通过，见[证据](evidence/query-read-unit-review.json)。这是受控ReadUnit的组合验证，尚非真实数据库/正式工厂，AUTHORITY-01/05/06保持开放。

## 11. 内核能力装配及元数据归属

新增平台能力名query_read_unit，Go内核按QueryReadUnit类型检查装配。查询应用要求snapshot.fixed_view与lifecycle.scoped_read两项特性；工厂交付ProtectUnit后的服务，并自行实现健康与关闭。内核仍不证明自报特性真实，真实仓储必须通过AUTHORITY门槛后进入允许清单。

2项内核测试使用两个受控测试提供方，保持同一QueryReadUnit调用、固定各自观察点及回调句柄自动关闭；provider停止后旧服务不能进入新回调。缺特性与错误接口被拒绝。全Go测试/vet通过，见[证据](evidence/query-read-module-review.json)。不是两种真实数据库替换、数据迁移或正式服务工厂。

只读适配器本身可不持有知识权威状态，但其数据源必须与MetadataRepository/UnitOfWork的权威数据归属相同，核验同一store_epoch和已迁移模型/水位。不能把读仓储指向另一个旧库后仍声明“替换了无状态模块”。替换底层元数据库时，读、写和事务服务共同切换并验证；不同连接池/实例允许，但不能各自决定有效发布事实。

本次仅扩展内部Go能力集合。部署Schema固定bindings尚无query_read_unit必需角色，正式归一化、数据源身份/BuildLock一致性、远程读协议和生产factory仍待设计/验证。不能把kernel.Build实验当作已完成部署配置接入。新能力属于未冻结V1草案，不默默改变已发布调用契约。

## 12. 部署角色与声明特性补齐

部署Schema现要求bindings.query_read_unit，capability固定为同名角色，required_features必须包含snapshot.fixed_view和lifecycle.scoped_read。离线装配校验进一步核对实例存在、能力契约和清单特性；不能只填一个任意provider名字或把metadata_uow当成读端口。更新的design-deployment及design-provider-manifest仍是假设实现，未注册真实factory。

3项Python新增检查覆盖缺角色/缺必需特性/错误能力、提供方缺实际声明特性及缺能力/不存在实例；全部177项Python回归通过。check_config对更新示例输出offline_validated且ready_for_service=false，见[证据](evidence/query-read-deployment-review.json)。这些是结构和自报清单检查，不证明观察点特性真实。

这是尚未冻结V1的新增必需部署字段；已有草案配置升级时必须补齐获准读实现和特性，不能自动替入假设提供方。BuildLock数据源身份、读写同一store_epoch/迁移水位、生产配置归一化与真实factory仍未验证；第11节记录的部署角色缺口已局部补齐，但AUTHORITY-06及完整运行验收不因此关闭。

读写数据源身份、独立控制面期望、提交点覆盖及恢复边界见[元数据存储绑定](METADATA_STORE_BINDING.md)。现有ReadIdentity三字段不能独立证明这些保证。


## 13. 另开观察点的有界当前批量读取

[ObserveCurrent](../../pkg/queryread/current.go)接收已准入QueryReadUnit，始终调用新的WithinRead，并使用ProtectUnit自动关闭句柄；不能将旧QueryReadRepository传入。先校验1–1000个唯一文档ID、有限ctx、Scope及预算，捕获新ReadIdentity并逐文档读取。身份字符串有长度/Unicode限制，编码结果总预算不超过4MiB；任何读取、范围、句柄、取消或预算失败均返回零结果，不将前半批当成完整当前复核。

这落实第8节“当前复核另开观察点”的规则，不是新增数据库适配器。固定快照用于一致准备；新调用负责重新观察。PostgreSQL的Repeatable Read保持事务原观察点，因此在原事务重读不能保证看到后来提交的撤回；Read Committed按语句取得观察点。依据[官方事务隔离说明](https://www.postgresql.org/docs/18/transaction-iso.html)。多文档复核还需统一当前批次观察点，不能混合数个语句版本后宣称全批一致。

助手只保证另开回调及输入/输出边界；新ReadVersion字符串本身不证明真实新鲜度。已准入适配器必须实际建立新的当前批次视图，不能重用缓存、历史快照或落后副本。相同版本/水位也可能只是没有新写入，不能单以字符串不同断言撤回可见。当前ACL需独立重新验证，收到状态后还须匹配PreparedSnapshot的发布/Revision/代际及Ready事实；本助手不自动授权或决定答案可见。

返回对象独立所有权遵循仓储契约，助手不深复制全部元数据；坏适配器共享可变返回对象仍是准入失败。真实SQL事务、控制源/服务认证、读写绑定及权限立即撤销尚未实现。

三项Go检查：先取旧状态、模拟撤回后通过另开回调读取新状态；途中读取失败/跨空间状态不返回部分数据；重复/空ID、无deadline零读取和字节超额拒绝。统一219项Python及Go/vet/三份OpenAPI通过。模拟回调不证明PostgreSQL隔离、当前ACL或生产撤回效果，AUTHORITY门槛仍开放。


## 14. 当前元数据与准备事实的关系比对

[MatchCurrent](../../pkg/queryread/match_current.go)接收独立保存的StoreEpoch/ScopeVersion/PublishedFact期望与另开观察点取得的CurrentObservation。先验证准备事实的基础发布关系、Scope范围及完整批次；重复/缺失/额外文档、无批准引用等结构失败返回ErrInvalid。主体/授权来源及父子请求身份须由应用独立验证，不能从传入Scope字段认定真实权限。

逐文档核对空间/KB、当前未删除、PublicationEpoch、有效Revision/GenerationSet、后端epoch及每渠道Generation/Profile/EmbeddingSpec。撤回、换版、代际/策略变化返回静态ErrStale，不把旧候选改ID套成新证据。文档RowVersion不得回退；仅RowVersion前进且发布绑定保持相同时允许，避免草稿等无关元数据变动导致原发布版本不可读取。

这里只核对给定元数据关系，不核验实时批准状态、内容摘要的实际字节、后端Ready或授权/配置来源。返回nil不是输出许可；应用仍需当前批准、实时索引验证、主体/策略/配置检查和发送批次边界。仓储实际新鲜度、Scope真实性、读写同源及多服务一致性保持既有准入要求。

三项Go测试覆盖八类过时状态、无关行版本前进/回退及策略变化、缺失/重复/外来批次和非法准备事实。统一219项Python及Go/vet/三份OpenAPI通过。未挂接真实QueryAuthority/后端，AUTHORITY与完整运行验收仍开放。


## 15. 应用复核编排

身份刷新、发布比对、当前批准、真实Ready/引用与最终版本检查的顺序及端口缺口见[CURRENT_CHECK_FLOW](CURRENT_CHECK_FLOW.md)。CurrentDocumentState已补当前批准/审核规则/Revision摘要投影及MatchCurrentReview局部比对；真实权威投影提供者仍待实现，这些局部助手不等同运行中的CheckCurrent。六项CHECK应用门槛保持开放。


## 16. 刷新主体与范围比对

当前复核前使用[MatchRefreshedScope](../../pkg/queryread/refreshed_scope.go)检查原认证操作与刷新Principal/Scope的身份、委托、动作、空间、权限版本与范围关系。ScopeID/已验证证明引用允许轮换，KB只可缩小且须覆盖必要来源。空证据仍检查，取消及无父deadline拒绝。三项Go测试覆盖局部关系；真实认证、策略来源、原值冻结及业务挂接未实现，不能据此关闭AUTHORITY或CHECK-01。


## 17. 局部当前复核编排

[CheckCurrent](../../pkg/queryread/check_current.go)串联两次控制刷新、身份/范围检查、新批次读取、批准/冻结关系和真实后端核验端口。四项Go模拟测试覆盖顺序、失败停止、控制状态变化、空证据与取消。实现要求有限父预算、复制可变输入、静态端口错误；nil仅表示本次观察通过。真实控制/读仓储/后端端口及业务输出调用未接入，不关闭AUTHORITY或CHECK运行门槛，详见[实现边界](CURRENT_CHECK_FLOW.md#8-局部编排实现与验收边界)。


## 18. PostgreSQL默认权威读取实现（2026-10-15）

此前章节中的“真实读仓储待实现”为当时边界；现已实现[QueryReads](POSTGRES_QUERY_READS.md)，同一主库只读Repeatable Read观察发布头、批准与当前规则、正式准备产物和后端代际。实际撤回/撤销批准、同快照分页与新读取，以及store轮换已有实库探针和独立返回关系复核。句柄与游标限制在回调内，返回独立复制。

默认范围最多1000个有效发布头，超限拒绝；旧来源缺少本平台正式发布证据时排除。部署需显式初始化/轮换store head，后端定义补Number及CutoverWatermark；缺字段的旧配置需创建合格新定义并迁移。它不是完整QueryAuthority服务；实际检索、真实IAM、正文及索引后端证据、最终输出、生产装配、跨仓储替换/恢复保持待验。AUTHORITY六项门槛仅取得部分子项证据，不整体关闭。


## 19. 当前发布关键词检索接入

[PostgreSQL默认Retriever](POSTGRES_KEYWORD_RETRIEVER.md)已接到标准检索接口，并复用读层内部同一事务核验发布事实及真实封存索引。它强制可信发布投影/调用授权回调，查询后重新授权、另开读观察点并调用MatchCurrentReview。当前实际授权/投影提供者为实库测试替身；生产Prepare/子授权、Grant账本及完整QueryAuthority服务尚未实现。实际候选也不能替代最终正文/索引证据和输出前当前权限复核。


## 20. 实际当前证据后端

[原生EvidenceBackend](POSTGRES_EVIDENCE_BACKEND.md)与真实QueryReadUnit/FrozenContentReader及本地原件接入CheckCurrent，Fresh观察点核验元数据与实际索引，期间物理/逻辑删除均拒绝。生产CurrentControl、完整QueryAuthority/子授权及Grant账本、模块装配与实际产品输出仍未验收，不能将当前组合探针等同生产查询授权服务。
