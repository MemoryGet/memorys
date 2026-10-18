# 历史正文读取：权限、来源与输出边界

更新：2026-10-17。此增量实现已发布历史版本的读取，不代表完整V1或生产部署验收。伙伴ReadRequest、ReadResult和knowledge_read工具Schema保持不变。

## 历史版本的准入规则

省略revision_id仍读取当前有效版本；显式传入当前版本不额外申请历史权限。显式旧版本要求当前文档可读，另以同一已认证Principal申请文档资源的`knowledge.history.read`。`knowledge.read`不能隐含历史权限，历史动作也不进入当前QueryReadUnit或搜索执行器。

目标必须有原生正式发布记录，并与平台文档/版本、发布意图、原始批准绑定；仅提交或仅批准但从未发布的版本拒绝。正常换版不会抹去旧版本的可追溯性。目标发布之后若有针对该版本的明确withdraw/delete历史，则拒绝旧目标；以后显式重新发布同一版本时，以最新有效发布记录重新判断。

文档当前撤回、删除或当前发布审核无效时，历史版本也拒绝。历史批准必须在当前审核规则下仍有效，批准撤销、规则失效、归属或策略变化均不能用旧游标绕过。此策略要求文档有当前有效发布版本，不开放未发布文档的归档正文。

## 可替换端口与默认实现

标准端口为HistoricalContentReader、HistoricalContentVerifier；请求同时携带当前read范围、独立history范围和应用已认证Principal，均为内部标准对象，不能从HTTP工具参数传入。CurrentReadService的History依赖组是可选且必须完整装配，缺少任何端口时拒绝构造；未装配时旧版本继续明确拒绝，不降级到当前正文。

PostgreSQL HistoricalFrozenReader每次真实重新授权两种动作，并在同一只读快照核对当前发布、目标历史发布及当前审核规则。正文由实际持久化Revision重建，标准JSON逐字及双摘要检查后，再开新观察点复核；历史读取不依赖旧搜索索引或旧后端仍在线。当前读层和历史读层共享当前审核关系加载，但没有放宽搜索动作。

TextHistoricalReadVerifier沿用当前文本来源验证能力：非空粗定位文本，revision引用验证批准正文，original引用验证独立授权的实际对象字节和投影。每一次底层冻结正文重读都保留历史范围及目标发布/批准绑定，不能把历史请求改成当前读取。元数据预期值使用独立深副本，提供方在验证期间修改原map不能同时改写验证基准；已有先失败再修复的回归测试。

## 应用、游标与最终控制

应用先取得当前文档归属/发布事实与当前配置，再申请独立历史资源授权，读取并验证目标发布、批准、正文双摘要和来源。签名游标同时绑定当前发布快照与目标历史发布/批准/摘要；改变显式revision_id无法继续已有游标。当前版本游标的绑定编码不因未启用History而改变。

来源验证和签名完成后，再次认证、重新定位当前read授权、复核当前规则/发布，并重新申请history授权和读取历史元数据。历史批准或授权在签名期间失效时，不返回正文或游标。游标期限取当前read、history与配置TTL的较早者，续页不延长初始期限。REST和MCP复用同一应用及伙伴映射，没有独立的历史权限旁路。

## 当前验证与证据边界

2026-10-18删除生命周期增量：已接入[实际软删除服务及事务](DOCUMENT_DELETION.md)。八个epoch的重发试验之后，通过真实删除命令提交epoch 9的tombstone、历史、回执、审计、事件及当前源清理计划；随后REST/MCP旧游标和新请求、当前正文及再次发布均拒绝。实际原件字节和三个Revision保留，删除不等于Purge。547项输入在PG16通过275项检查，删除/历史/当前独立复核及全量本地检查通过。最新证据见[删除复核](evidence/document-deletion-runtime-review.json)、[历史回归](evidence/document-deletion-history-read-review.json)。下面各轮计数只证明对应历史快照。

2026-10-18协议生命周期增量：在相同八个epoch试验中启动真实localhost REST及官方SDK MCP入口。重发前后各由契约路径/api/v1/knowledge-os/read返回首块及游标，再经MCP knowledge_read继续第二块。文档撤回、其他版本发布后，两个入口均返回安全错误；显式重发目标再换版后，旧REST游标在REST返回409/conflict，在MCP返回isError及同类安全错误。新的REST游标仍可继续MCP次页。独立复核五阶段实际HTTP/RPC样本：请求Schema、两页完整正文/引用/配置摘要、MCP结构化与文本一致、续页无额外游标，以及错误仅含code/safe_message/request_id/retryable，不携带正文、引用或游标。

入口采用从主库当前配置目录解码并校验的可信配置快照；测试Search服务仅为MCP目录装配提供既有端口，本轮不调用或验收搜索。服务在fixture阶段结束时关闭，凭据副本按已有退出路径清除。未增加业务预算、任务租约或原件权限。

当前541项输入在PG16通过261项原生检查，全量Go测试/go vet、228项Python及三份OpenAPI通过；历史/当前读取独立复核及源码快照一致性通过，临时目录与端口已独立确认清理。见[协议生命周期实库证据](evidence/history-transport-lifecycle-postgres16-review.json)、[独立复核](evidence/history-transport-lifecycle-runtime-review.json)、[当前读取回归](evidence/history-transport-lifecycle-current-read-review.json)、[本地检查](evidence/history-transport-lifecycle-local-checks.json)及[资源清理](evidence/history-transport-lifecycle-cleanup-review.json)。以下各轮记录仅证明其自身历史快照。

2026-10-17应用生命周期增量：同一真实RS256/主库状态认证及CurrentReadService贯穿八个epoch的试验。目标正文包含两块，正常换版时可完成初页及续页；文档明确撤回时，带旧游标或不带游标均拒绝；发布另一版本后，已明确撤回的目标仍拒绝。显式重新发布目标再换版后，新请求可完成两页，旧游标明确返回ErrReadCursor，正文、块选择和续页游标均为空。独立校验四份实际应用页面与冻结正文、块ID顺序一致，新旧游标不同，并保留底层实际历史/批准/ACL复核。

回调从主库当前配置头解析可信请求摘要，不能沿用此前配置切换试验留下的旧摘要；首次旧fixture摘要被应用拒绝的记录保留为history-app-postgres16-stale-fixture-profile-failed.json。跨阶段凭据单独复制并在整个查询/生命周期阶段退出时清除，包含失败退出路径，不输出访问令牌。原件/治理授权仍为fixture；本轮目标为revision来源文本，未扩大原件授权或业务预算。

当前540项输入在PG16通过258项原生检查，全量Go测试/go vet、228项Python及三份OpenAPI通过；历史/当前读取独立复核通过，所有报告与同一源码快照匹配，临时目录和端口已独立核对清理。见[应用生命周期实库证据](evidence/history-app-lifecycle-postgres16-review.json)、[独立复核](evidence/history-app-lifecycle-runtime-review.json)、[当前读取回归](evidence/history-app-lifecycle-current-read-review.json)、[本地检查](evidence/history-app-lifecycle-local-checks.json)及[清理核对](evidence/history-app-lifecycle-cleanup-review.json)。以下255/252/250项记录为各自历史快照。

2026-10-17后续增量：在既有查询/输出阶段结束后，新增独立20秒的历史生命周期fixture阶段。通过真实提交后的既有批准、Start/Claim/Prepare/Complete/Finalize和Withdraw服务，记录同文档epoch 1至8：正常换版时读取epoch 2历史正文；epoch 4再次发布该目标，epoch 5明确撤回后整份文档的历史读取拒绝；epoch 6发布另一版本后，目标仍拒绝；epoch 7显式重新发布目标、epoch 8正常换版后，历史读取恢复并绑定epoch 7的新发布记录。恢复前后的冻结字节与批准保持一致，不能将epoch 2的旧发布记录当作重新生效依据。

此增量直接验证HistoricalFrozenReader端口，读取权限使用新封存的策略3，保持策略头单调递增，不回滚此前已撤销的策略1。治理写入授权仍为fixture；没有扩展任务4秒租约、业务请求预算，也没有证明旧应用游标恢复、发送撤权屏障或删除生命周期。独立校验实际八条历史的文档/版本/previous关系、撤回回执、发布ID/epoch、冻结正文双摘要、批准关系及两条实际读取授权。

本轮540项输入在PG16通过255项原生检查，全量Go测试/go vet、228项Python与三份OpenAPI通过；历史生命周期与当前读取独立复核通过，临时目录和端口已另行核对清理。见[生命周期实库证据](evidence/history-lifecycle-postgres16-review.json)、[独立复核](evidence/history-lifecycle-runtime-review.json)、[当前读取回归](evidence/history-lifecycle-current-read-review.json)、[本地检查](evidence/history-lifecycle-local-checks.json)及[资源清理](evidence/history-lifecycle-cleanup-review.json)。下面539/252与539/250记录保留为历史快照，不代替当前源码证据。

2026-10-17增量：新增第二位真实RS256阅读者及主库令牌登记，在同一封存策略中分别授予read/history。第二主体先通过REST成功读取相同历史版本的首块和引用，再尝试复用第一主体游标；应用必须返回ErrReadCursor，且正文和续页游标均为空。独立复核逐项核对第二主体、原生来源请求、两条实际授权、未撤销令牌登记、正文一致及不同的独立游标。原件控制和本地对象存储的fixture仅扩展到这两位指定阅读者，仍核对原对象及原有范围；不代表生产原件Grant。

当前539项源码输入在PG16通过252项原生检查，全量Go测试/go vet、228项Python及三份OpenAPI通过；历史与当前读取独立复核通过，所有报告匹配相同源码哈希，临时目录和端口已独立确认清理。见[本轮实库证据](evidence/history-subject-postgres16-review.json)、[跨主体独立复核](evidence/history-subject-runtime-review.json)、[当前读取回归](evidence/history-subject-current-read-review.json)、[本地检查](evidence/history-subject-local-checks.json)及[清理核对](evidence/history-subject-cleanup-review.json)。以下250项记录保留为上一快照。

本轮先因重复插入测试授权失败，去重修正；另一次在既有completion-renewed租约场景提前失败，未执行到新增检查。失败报告保留为history-subject-postgres16-duplicate-fixture-failed.json和history-subject-postgres16-existing-lease-failed.json。最终单独运行实库探针通过，不构成间歇失败根因或稳定性验收结论。

定向测试已覆盖独立授权拒绝、错误动作/KB/策略、未发布或错误版本、历史批准撤销、来源失败、签名期间撤权/撤销历史批准、最终历史授权拒绝、历史分页及输入/预期元数据副本隔离。当前动作白名单测试明确拒绝`knowledge.history.read`。

原生试验通过真实提交、人工审核fixture、准备/完成和发布服务，先发布原件版本，再发布同文档的新Revision；另生成已批准但未发布的第三版本。随后删除旧索引，通过实际RS256、PG封存的read与history双授权读取旧正文，REST初页游标继续MCP次页，并验证更换目标、缺失目标、批准未发布及实际撤销旧批准的拒绝。

539项源码输入在PG16通过250项原生检查；全量Go测试/go vet、228项Python及三份OpenAPI检查通过。历史读取与已有当前读取均通过独立来源、字段及Schema复核，临时实例目录和端口已独立确认清理。本轮仅PG16，未沿用旧快照声称PG15/18同步通过。见[实库证据](evidence/historical-read-application-postgres16-review.json)、[独立历史复核](evidence/historical-read-runtime-review.json)、[本地检查](evidence/historical-read-local-checks.json)。

独立复核需要逐项检查：两条实际授权行与Principal/Scope绑定；两次正式发布的文档/版本及先后关系；实际历史冻结Schema、双摘要、批准/撤销关系；旧索引删除的实际状态；整块原文与实际对象字节；ReadRequest/ReadResult及MCP双格式、分页完整性和引用字段。不同源码快照的旧绿色记录不能代替此轮证据。

## 仍未验收

生产密钥/issuer、授权与审核管理、生产原件Grant及发送撤权屏障、完整默认服务装配、结构媒体/精确来源、真实大库并发与延迟/成本、封存中文Recall、完整替换/迁移/回退均保持开放。[删除管理入口](MANAGEMENT_DELETE_RUNTIME.md)已接入，删除任务查询/消费、物理Purge、跨issuer/客户端及完整生命周期实库矩阵仍需补验；两个指定主体的游标隔离和顺序REST/MCP/软删除生命周期不能宣称覆盖全部并发/故障场景。读取业务拒绝目前沿用安全通用错误，细化错误分类仍待完成。

原件读取授权、治理写入授权和旧索引清理授权在探针中仍是受控fixture，不等同生产Grant/IAM。历史阅读者的令牌认证与read/history授权由真实签名及主库封存ACL执行；必须分别陈述这些证据。
