# 管理任务查询：公开状态与资源权限

后续进度：[实际清理消费者](DELETION_CLEANUP_RUNTIME.md)及[队列发现/轮询](DELETION_QUEUE_RUNTIME.md)已贯通，任务状态包含动态running/completed/failed。最新574项/309检查与本页状态/权限回归均通过，早期初始queued限制保留为历史证据。

更新：2026-10-18。当前实现读取实际删除任务的初始定义与持久化运行进度，已接入[有界清理消费者](DELETION_CLEANUP_RUNTIME.md)的running/completed/failed状态；不是完整TaskRepository或持续队列调度系统。

## 标准端口与应用

contracts.ResourceJobReader是独立窄读端口，返回标准Job和实际ResourceID。JobReadRequest的Principal由JobStatusService通过Authenticator取得，不能从HTTP传入。接口没有伪装实现TaskRepository的Create/Claim/Renew/Complete/Cancel/Retry。

JobStatusService复制凭据，每次认证都提供独立副本并清除。应用先认证、读取已授权任务，再认证并重读，最后再次认证。提供方请求和结果独立复制，绑定任务ID、实际资源、空间、操作、配置和阶段；版本不得回退，同版本不能改变内容。不同主体、错误任务、资源漂移、期间令牌撤销或无效数据均不返回状态。

应用允许标准来源返回有更高版本的状态。PG适配器将不可变删除任务定义与独立运行表组合，核对版本/计数/状态；新观察点若进度前进，可返回更高版本，但不允许身份、资源、配置、阶段漂移或同版本内容变化。实际running/completed/failed已通过默认PG关键词索引消费者验证，其他任务类型和完整运行管理仍待实现。

## PostgreSQL 来源与独立权限

DeletionJobReader从document_delete_jobs关联实际删除回执、committed命令和文档，解析真实文档/空间/KB；校验标准Job与持久化元数据相同。读取前后独立向Authorizer申请resource.view_job，资源是任务关联的真实文档，动作不是knowledge.read或kb.delete。新观察点重读任务及关联关系，返回前再次核权并拒绝策略版本变化。

任务所属文档已软删除仍可查询，前提是请求者当前具备独立任务查看权限。删除权限不隐含任务查看权限，任务查看也不隐含正文读取或删除权限。关联记录不可变，不能把任务改绑到另一文档后复用旧授权。

此来源只作为内部可信应用端口，不能单独接受未经认证的客户端Principal。严格授权/令牌撤销与输出之间的Grant及发送屏障仍未完成，逐次核验不能代替生产一致性屏障。

## HTTP 映射

GET /api/v1/knowledge-os/jobs/{id}按管理OpenAPI返回Job：id、space_id、resource_id、state、stage、attempt、row_version和progress，可选标准安全error。resource_id来自实际任务关联文档，不使用job_id或operation_id代替。私有操作ID、配置摘要、代际、fencing token及租约不进入响应。

请求只接受任务路径ID和Bearer认证，拒绝查询参数、非空正文及错误方法，不接受空间、资源、过滤器或主体注入。任务缺失、无权限、跨KB或权限撤销统一返回404安全错误，令牌失效返回401；依赖错误503，期限超时504。成功与错误均no-store并提供X-Request-ID。默认服务注册该路由但未装配时返回503/capability_missing，无需启用搜索或查询配置源。

## 当前验证

最新有界消费增量为568项输入、PG16共302项检查，真实7目标清理、HTTP逐步进度与completed、零目标完成及unknown隔离后的failed通过独立复核；全量本地及既有回归通过。见[运行进度与清理证据](evidence/deletion-cleanup-runtime-review.json)。以下561项记录是原任务查询快照。

561项输入在PG16通过291项原生检查，Go全量测试/go vet、228项Python及三份OpenAPI通过。管理删除、历史和当前读取独立复核同步通过，所有报告对应同一源码快照，临时目录和端口已独立确认清理。

原生试验使用真实RS256/主库状态与封存策略6：查看者仅有resource.view_job，无read/delete权限；另一主体仅有kb.delete，不能查看任务。实际已删除文档的queued任务返回200；任务缺失、另一个真实KB下的任务及删除专用主体返回相同404安全错误。令牌提前或在读取期间撤销返回401；读取期间将权限切换至空策略7，返回前即阻止状态，后续请求仍404。直接改变任务归属的SQL更新被拒绝。跨KB任务通过真实删除服务产生，创建权限明确为治理fixture，查看权限则由真实ACL执行。

独立Python按管理Job/Error Schema验证实际HTTP响应，逐字段比对持久化任务、删除回执及真实文档，检查唯一权限行/主体/issuer/client/KB和安全错误一致性。应用/入口测试覆盖反复认证、主体与资源绑定、错误ID、版本回退、期间撤权、输入与凭据副本隔离、私有字段不投影、注入拒绝及默认未装配拒绝。

见[实库证据](evidence/job-status-postgres16-review.json)、[独立任务复核](evidence/job-status-runtime-review.json)、[本地检查](evidence/job-status-local-checks.json)、[删除回归](evidence/job-status-deletion-review.json)、[历史回归](evidence/job-status-history-read-review.json)、[当前读取回归](evidence/job-status-current-read-review.json)及[资源清理](evidence/job-status-cleanup-review.json)。本轮仅PG16，旧PG15/18快照不能代替本轮源码证据。

生产Bootstrap/IAM、读取审计及完整管理UI、其他类型任务读取、持续调度与更多后端的运行进度、重试/人工对账、完整TaskRepository、原件Purge、故障/并发矩阵、发送屏障与完整V1仍未验收。实际令牌和ACL依然使用临时测试登记，不能称生产身份管理已完成；既有任务租约间歇失败保持开放。
