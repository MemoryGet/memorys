# 已实现SQL与事务的PostgreSQL多版本验证

2026-10-16，在独立WSL临时实例中执行相同SQL脚本和235项Go仓储/应用链路检查。三次使用相同509项源码输入和同一个Go探针二进制，每次从空库初始化，不进行跨版本数据目录复用。

| 服务端实际版本 | SQL脚本 | 同一套235项检查 | 服务停止/目录清理 |
|---|---|---|---|
| 15.19（PGDG Ubuntu包） | 通过 | 通过 | 已核对 |
| 16.15（Ubuntu包） | 通过 | 通过 | 已核对 |
| 18.6（PGDG Ubuntu包） | 通过 | 通过 | 已核对 |

覆盖当前实现的撤回事务、历史/任务/清理、索引删除与快照交接、候选CAS/回执/审计/outbox/读取，以及真实文件解析后的候选应用链路，以及数据库草稿到冻结Revision和submitted/版本0审核请求的同步原子提交，以及批准/拒绝/撤销批准的持久决定与当前指针，以及当前规则/后端绑定与发布准备登记、持久租约/接管/续租及已授权冻结内容读取，以及文本关键词索引准备、实际投影读回与attempt隔离，以及重新核验后的任务完成、原子回执/审计/事件和租约到期后的历史对账。包含回滚、重放、迟到结果保留与提交后丢失响应的受控故障试验。权限回调、初始业务元数据及故障注入仍是fixture。

新增伙伴搜索检查：实际契约路径POST、省略KB按当前授权、显式空列表不进入计划、失效身份/缺失策略目录拒绝及撤销令牌401，独立SearchRequest/SearchResult/Error复核通过。

新增重排预算检查：配置候选池不被用户Top K提前截断；真实索引两条候选中选择原第二名并只返回Top 1，独立来源复核通过。

新增融合边界检查：注入Fuser前后各一次实际后端证据验证，短阶段原件读取授权deadline收紧；原始候选形状校验及复核失败阻止调用的单元测试通过。

新增多路由检查：两路实际PG查询、RRF去重/贡献以及路由失败无部分输出；独立复算分数和配置。当前为同一PG实现的双路，非成熟混合召回质量验收，见[多路由说明](SEARCH_MULTI_ROUTE.md)。

新增重排检查：候选前后当前证据复核、受控测试重排器、伪造摘录拒绝与重排中撤销凭据最终拒绝。独立输出与配置关系复核通过；非成熟模型效果验收，见[重排说明](SEARCH_RERANKING.md)。

新增统一应用检查：真实认证/授权/当前配置、实际SQL检索及证据复核由SearchService执行，localhost HTTP适配调用，撤销令牌拒绝。计划源与准入由测试宿主提供，正式MCP/伙伴API尚未完成。见[应用服务](KNOWLEDGE_SEARCH_APPLICATION.md)。

新增查询装配检查：配置快照与具名实例/路由对应，实际数据库候选由装配实例返回，关闭后旧引用拒绝；独立样本关系验证通过。见[模块装配](PROFILE_MODULE_ASSEMBLY.md)。

新增配置目录检查：实际保存完整平台配置快照，重算索引/查询/流水线摘要，内容与键不符时拒绝；独立Python摘要复算通过，见[快照完整性](PROFILE_SNAPSHOT_INTEGRITY.md)。

新增当前事实源检查：主库配置/审核/授权头、不回传请求摘要、缺失或旧授权拒绝、不可变引用、配置切换后旧请求拒绝及指针倒退失败。仅验证当前引用，非完整配置编译与实际装配，见[当前事实源](POSTGRES_CURRENT_FACTS.md)。

新增持久化授权检查：实际封存ACL、默认拒绝、撤权、封存后追加失败、版本倒退失败及当前证据最终检查拒绝。见[授权设计](PERSISTENT_AUTHORIZATION.md)。

新增访问令牌认证检查：实际RS256签名及主库允许清单，撤销、主体/客户端/密钥停用、公钥绑定、未登记令牌和状态源失败均拒绝。发行方与授权策略仍为测试配置，见[认证说明](IDENTITY_ACCESS_TOKENS.md)。

```powershell
python docs/architecture/check_postgres_wsl.py --major 15 --port 55433
python docs/architecture/check_postgres_wsl.py --major 16 --port 55432
python docs/architecture/check_postgres_wsl.py --major 18 --port 55434
```

`--major`只允许15/16/18，且运行后核对SHOW server_version，拒绝请求与实际主版本不一致。各版本使用工作区内独立解包目录、回环端口、私有数据目录和所有权标记。PG18所需liburing也仅解包到该版本的运行目录，未安装系统服务或修改系统数据库。

PGDG来源与仓库公钥参照[PostgreSQL官方Ubuntu下载说明](https://www.postgresql.org/download/linux/ubuntu/)。使用独立apt索引、软件源文件和Signed-By文件，不注册系统软件源；软件包与公钥摘要保存于[PGDG制品记录](evidence/postgres-pgdg-packages.json)。这些身份记录不是完整运行环境/SBOM证明。

汇总证据：[postgres-version-matrix.json](evidence/postgres-version-matrix.json)。原始版本报告分别为[PG15](evidence/postgres15-wsl-review.json)、[PG16](evidence/postgres16-wsl-review.json)、[PG18](evidence/postgres18-wsl-review.json)，包含源文件、服务程序和Go二进制摘要。清理后另行确认私有目录不存在、测试端口已关闭。

该项通过的是**当前已实现子集的版本兼容验证**。不等同于完整Metadata UoW、审核发布、真实IAM/RLS、HTTP/MCP、生产性能、全量元数据迁移/回退验收。它不证明直接升级/降级已有数据目录，也不替代旧Docker镜像的精确发行版验证；旧Docker报告保留为历史记录。

当前追加[最终发布与替换](FINAL_PUBLICATION.md)16项检查，覆盖实时索引再核验、发布CAS及原子历史/回执/审计/outbox、固定旧源清理和既有撤回互操作。6份发布结果、12个公共投影和6个命令摘要通过独立对照；仍不代表正式服务、真实权限、更多检索通道或完整发布/检索产品验收。

当前再追加[查询权威读层](POSTGRES_QUERY_READS.md)12项场景：真实Repeatable Read多页、当前批准/代际、并发撤回与新读取失效、句柄关闭、游标与返回对象隔离、当前权限回调、取消、store轮换及实际撤销批准；3组真实观察投影通过独立关系校验。只证明元数据读取子集，不证明实际检索结果、完整QueryAuthority/CHECK、真实IAM或仓储替换。

本轮追加[当前发布关键词检索](POSTGRES_KEYWORD_RETRIEVER.md)14项场景：实际SQL、平台引用、未发布隔离、投影/范围/路线/代际约束、字面查询、确定排序/Limit、当前及最终权限回调、取消、撤回/批准撤销和实际索引删除、模块协议。当前3个实际候选与51条原生索引记录、3组投影摘要及模块Schema通过独立复核。只证明关键词候选检索子集，生产身份、最终输出/正文复核、第二检索引擎、Recall与性能仍开放。

本轮追加[当前发布正文读取](PUBLISHED_FROZEN_READER.md)8项场景：真实正文、类型化Revision重建、双摘要、返回值独立、空间/版本/草稿拒绝、迟到撤权、取消/期限，以及撤回/撤销批准后拒绝。当前12份实际标准正文通过独立Schema、canonical字节及双摘要核验；原件字节/媒体真实性、Evidence定位及完整CurrentBackend/CurrentControl仍待完成。

2026-10-16追加[实际当前证据后端](POSTGRES_EVIDENCE_BACKEND.md)11项场景。完整CheckCurrent接真实关键词快照、冻结正文和本地原件引用，检测投影期间物理删除以及逻辑分块删除保留物理行；11项场景及3组真实操作样本通过[独立复核](evidence/evidence-backend-runtime-review.json)。当前身份/控制提供者仍为fixture，不代表生产IAM、最终模型/API输出或完整架构验收。

2026-10-16同一477项输入下，原有PG235项回归与[成熟原件投影](OFFICE_ORIGINAL_PROJECTION.md)四格式/双PDF引擎验证分别通过。Office/PDF原件样本未接PG生命周期；实际PG仍使用UTF-8，两个范围不能合并成全格式产品验收。

2026-10-16接入[AuthenticatedControl](AUTHENTICATED_CURRENT_CONTROL.md)标准编排，正向证据链路两次实际调用Authenticator/Authorizer并读取DB规则。三组样本记录每次各两次调用并明确identity_provider=fixture；这项新增检查不证明生产令牌或用户权限已实现。

本轮新增6项MCP运行检查：协议初始化、REST搜索一致性、结构化/文本结果一致、参数伪造主体拒绝、协议版本白名单及撤销凭据在分派前拒绝。三版使用相同探针二进制；临时目录与端口已独立确认清理。受控私有Bearer，完整OAuth/客户端互操作及生产部署未验收。
