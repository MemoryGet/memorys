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

2026-10-17增量：新增第二位真实RS256阅读者及主库令牌登记，在同一封存策略中分别授予read/history。第二主体先通过REST成功读取相同历史版本的首块和引用，再尝试复用第一主体游标；应用必须返回ErrReadCursor，且正文和续页游标均为空。独立复核逐项核对第二主体、原生来源请求、两条实际授权、未撤销令牌登记、正文一致及不同的独立游标。原件控制和本地对象存储的fixture仅扩展到这两位指定阅读者，仍核对原对象及原有范围；不代表生产原件Grant。

当前539项源码输入在PG16通过252项原生检查，全量Go测试/go vet、228项Python及三份OpenAPI通过；历史与当前读取独立复核通过，所有报告匹配相同源码哈希，临时目录和端口已独立确认清理。见[本轮实库证据](evidence/history-subject-postgres16-review.json)、[跨主体独立复核](evidence/history-subject-runtime-review.json)、[当前读取回归](evidence/history-subject-current-read-review.json)、[本地检查](evidence/history-subject-local-checks.json)及[清理核对](evidence/history-subject-cleanup-review.json)。以下250项记录保留为上一快照。

本轮先因重复插入测试授权失败，去重修正；另一次在既有completion-renewed租约场景提前失败，未执行到新增检查。失败报告保留为history-subject-postgres16-duplicate-fixture-failed.json和history-subject-postgres16-existing-lease-failed.json。最终单独运行实库探针通过，不构成间歇失败根因或稳定性验收结论。

定向测试已覆盖独立授权拒绝、错误动作/KB/策略、未发布或错误版本、历史批准撤销、来源失败、签名期间撤权/撤销历史批准、最终历史授权拒绝、历史分页及输入/预期元数据副本隔离。当前动作白名单测试明确拒绝`knowledge.history.read`。

原生试验通过真实提交、人工审核fixture、准备/完成和发布服务，先发布原件版本，再发布同文档的新Revision；另生成已批准但未发布的第三版本。随后删除旧索引，通过实际RS256、PG封存的read与history双授权读取旧正文，REST初页游标继续MCP次页，并验证更换目标、缺失目标、批准未发布及实际撤销旧批准的拒绝。

539项源码输入在PG16通过250项原生检查；全量Go测试/go vet、228项Python及三份OpenAPI检查通过。历史读取与已有当前读取均通过独立来源、字段及Schema复核，临时实例目录和端口已独立确认清理。本轮仅PG16，未沿用旧快照声称PG15/18同步通过。见[实库证据](evidence/historical-read-application-postgres16-review.json)、[独立历史复核](evidence/historical-read-runtime-review.json)、[本地检查](evidence/historical-read-local-checks.json)。

独立复核需要逐项检查：两条实际授权行与Principal/Scope绑定；两次正式发布的文档/版本及先后关系；实际历史冻结Schema、双摘要、批准/撤销关系；旧索引删除的实际状态；整块原文与实际对象字节；ReadRequest/ReadResult及MCP双格式、分页完整性和引用字段。不同源码快照的旧绿色记录不能代替此轮证据。

## 仍未验收

生产密钥/issuer、授权与审核管理、生产原件Grant及发送撤权屏障、完整默认服务装配、结构媒体/精确来源、真实大库并发与延迟/成本、封存中文Recall、完整替换/迁移/回退均保持开放。明确撤回后重新发布、删除、跨issuer/客户端及完整历史生命周期实库矩阵仍需补验；本轮两个指定主体的游标隔离不能宣称覆盖全部场景。业务拒绝目前沿用安全通用错误，细化错误分类仍待完成。

原件读取授权、治理写入授权和旧索引清理授权在探针中仍是受控fixture，不等同生产Grant/IAM。历史阅读者的令牌认证与read/history授权由真实签名及主库封存ACL执行；必须分别陈述这些证据。
