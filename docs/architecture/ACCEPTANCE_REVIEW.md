# AI Knowledge OS V1 架构设计验收报告

验收日期：2026-10-12。依据为 [原始方案](../AI_Knowledge_OS_开发计划与模块化架构.md)、现有设计文件、机器契约、Go源码和本次重新执行的检查。用户要求本轮验收架构设计，未将真实组件与产品验收从总目标中删除。

## 1. 结论与适用范围

**总体架构评审有条件通过；V1契约冻结及原方案的完整架构验收暂未通过。**

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
