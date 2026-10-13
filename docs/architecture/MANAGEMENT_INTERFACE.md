# 知识管理界面与管理应用契约 V1

管理界面是标准 API 的客户端，不能直接调用 WeKnora 或查询其数据库。Vue 是初始实现；替换 UI 时使用相同权限、状态和命令契约。本文描述信息结构与行为，管理OpenAPI草案已生成并通过规范校验；页面视觉和运行实现留在下一阶段。

## 1. 信息结构

| 页面 | 展示对象 | 操作 | 状态要求 |
|---|---|---|---|
| 空间/知识库 | 成员、KB、角色、启用的 Profile | 创建 KB、授权、选择允许的 Profile | 只列出可访问资源；普通成员看不到部署密钥和全局模块配置 |
| 文档目录 | 文档、当前发布版本、当前草稿、处理/审批状态 | 上传、查看、建新草稿、撤回、删除 | 三种状态分开展示，不能用单个“成功”表示已发布 |
| 草稿编辑 | DraftBlock、表格、图片、原件来源、诊断 | 编辑、替换原件、重新解析、提交 | 提交前版本 CAS；冲突显示版本差异，不覆盖别人修改 |
| 审核工作台 | 冻结 Revision、与当前发布版差异、来源、审批记录 | 批准、拒绝、撤销审批 | 审批绑定冻结内容哈希；批准不等同索引已准备或已发布 |
| 发布与历史 | 版本、审批、发布操作、代际和撤回原因 | 发布、查看历史、从旧版本复制草稿 | 发布失败仍展示旧有效版；从旧版恢复要走新的审批发布 |
| 检索/问答测试 | 最终证据、引用、配置摘要、耗时、降级 | 搜索、问答、提交反馈 | 普通用户只看合法证据；内部诊断需单独管理权限 |
| 反馈工作台 | 用户意见、建议 Draft、处理状态 | 生成建议、采纳入草稿、拒绝建议 | 采纳不自动批准或发布；访问目标重新鉴权 |
| 任务中心 | Job、阶段、进度、安全错误、重试/取消状态 | 重试失败任务、取消可取消任务 | 不显示后端密钥或其它空间任务；取消不撤销已经发布的内容 |
| 管理与运维 | 模块清单、配置版本、迁移记录、观测摘要 | 查看健康、导出、受控迁移 | 部署级权限单独授予；不向 KB 管理员开放任意插件装配 |

## 2. 管理 API 资源和命令

所有路径以 `/api/v1/knowledge-os` 为前缀。字段语义使用 [标准契约](CONTRACTS.md)，写入幂等和乐观锁使用 [逻辑数据模型](DATA_MODEL.md)。响应区分对象 row_version、发布 publication_epoch 和配置 profile_digest，不能混成单个 version。

| 操作 | 方法与路径 | 关键输入 | 返回与权限 |
|---|---|---|---|
| KB 列表/创建 | GET/POST /spaces/{space_id}/knowledge-bases | 游标或名称 | 列表按主体过滤；创建需要空间管理权限 |
| KB 授权变更 | PUT /knowledge-bases/{kb_id}/grants/{subject_id} | role、expected_policy_version、幂等键 | 新 policy_version；Owner/Admin，限制授予范围 |
| 文档上传 | POST /knowledge-bases/{kb_id}/documents | multipart 原件、title、幂等键 | 202 Document/Draft/Job；Contributor及以上 |
| 文档列表/目录读取 | GET /knowledge-bases/{kb_id}/documents、GET /documents/{id} | 游标或 ID | 目录元信息按查看/编辑权限过滤，不携带无权访问的草稿内容 |
| 新建编辑草稿 | POST /documents/{id}/drafts | base_revision_id、expected_document_version、幂等键 | Draft；不修改原 Revision |
| 草稿读取/编辑 | GET/PATCH /drafts/{draft_id} | 编辑传 expected_version、明确 Block 变更 | 新 row_version；冲突409；编辑权限必需 |
| 换原件/重解析 | POST /drafts/{draft_id}/source、POST /drafts/{draft_id}/parse | 附件或解析 Profile、expected_version、幂等键 | 202 Job；迟到结果遵循版本检查 |
| 提交审核 | POST /drafts/{draft_id}/submit | expected_version、幂等键 | 冻结 Revision、审批请求；422用于不满足内容完整性等领域校验 |
| 待审列表/冻结内容 | GET /knowledge-bases/{kb_id}/approval-requests、GET /revisions/{revision_id} | 游标或 Revision ID | 审核人获取审批版本和内容哈希；作者读取自己的提交也需revision.review授权 |
| 审批决定 | POST /revisions/{revision_id}/approval-decisions | decision、reason、expected_approval_version、幂等键 | Approval；Owner/Admin；允许 personal owner 确认 |
| 发布 | POST /documents/{id}/publications | revision_id、approval_id、expected_publication_epoch、幂等键 | 202 PublicationOperation；最终状态通过任务/操作查询 |
| 撤回 | POST /documents/{id}/withdraw | reason、expected_publication_epoch、幂等键 | 200 新 epoch 和撤回记录；明确停止历史读取 |
| 软删除 | DELETE /documents/{id} | expected_document_version、幂等键 | 202 删除操作；立即停止访问，物理清理另行执行 |
| 历史/原件 | GET /documents/{id}/history、GET /blobs/{blob_id}/content | 游标或 BlobRef | 分别校验历史/原件权限；原件通过授权代理返回 |
| 任务/发布操作 | GET /jobs/{id}、GET /publication-operations/{id} | ID | 进度和安全诊断；按所属资源鉴权 |
| 任务取消/重试 | POST /jobs/{id}/cancel、POST /jobs/{id}/retry | expected_job_version、幂等键 | 明确 accepted/already_terminal/not_cancellable；重复不新增副作用 |
| 反馈列表/处理 | GET /knowledge-bases/{kb_id}/feedback、POST /feedback/{id}/proposals | 游标或意见和幂等键 | 合法反馈/建议 Draft；不提供自动发布路径 |

上表是管理接口设计清单，不表示路由已实现。正式 OpenAPI 必须为请求、响应、分页、错误和示例生成完整 schema。伙伴四工具仍使用统一应用服务，不能通过同一服务 Key 自动获得全部管理权限。

对应的 [管理OpenAPI草案](openapi/management.v1.json) 已覆盖26项操作。机器定义从 `build_management_contract.py` 生成，公共Block/Blob/来源/错误类型从伙伴OpenAPI复用；执行时加 `--check` 可检查生成文件是否过期。管理写命令统一使用必需的Idempotency-Key请求头；反馈伙伴工具继续使用body.idempotency_key。删除的expected_document_version放在query参数中，不要求DELETE请求体。

草稿PATCH的V1语义是基于expected_version替换有序blocks集合；不是未经定义的JSON Patch。每个Block校验归属及结构，省略的旧Block从该草稿移除，旧Revision不变。新增Block ID由平台受控分配/确认；不能用其它草稿的ID覆盖其数据。若未来改成细粒度增删命令，需新契约版本。

审批请求初始row_version=0；批准/拒绝/撤销决定后递增，expected_approval_version比较请求的当前版本，不是目标Revision版本。投稿人没有审批权限时不能读取其它人的审核队列。审批列表、冻结版本读取与普通已发布知识读取采用不同动作，Viewer不能通过管理端读取草稿。

## 3. 权限不以页面隐藏为依据

导航和按钮按能力显示，后端每个命令仍重新授权。角色是初始策略模板：Viewer 默认只能查询已发布内容；Contributor 能编辑所属可写 KB 的草稿；Owner/Admin 有审批发布与授权管理资格。空间 Owner 管理成员生命周期，KB Admin 不因此能管理其它 KB 或授予空间 Owner。

草稿预览使用管理读取端口，不调用普通检索接口模拟未发布检索。需要草稿检索测试时使用独立受控 Preview 服务和候选代际，不进入正式召回集合，结果明确标记 preview，默认不能通过伙伴 MCP 获取。

批量操作按每个对象鉴权、CAS 和结果返回，默认不承诺跨文档全有或全无。批量审批必须逐项绑定内容哈希与明确的人类决定；选择“全选”不能授予此前未加载/未核验版本的隐式审批。

## 4. 异步与冲突体验

页面收到202后跟踪 Job/PublicationOperation，展示 queued/running/failed/ready/committed 等实际状态。轮询使用退避和停止条件，页面关闭不取消后台任务。未来 SSE 推送只减少轮询，任务事实仍以 API 读取为准。

草稿409返回安全的 current_version 和重新获取地址，前端展示差异供用户选择合并；不自动重放覆盖。发布 CAS 冲突必须重新读取当前发布状态，再由用户选择操作。提交后界面编辑产生新的草稿，不继续修改已冻结对象。

## 5. 扩展与替换验收

- UI 换成其它框架，上传→草稿→审核→发布→读取→撤回的应用结果保持相同。
- UI 与 MCP 使用同主体授权，不能出现页面无权限但工具可读取的差异。
- 前端仅显示平台 citation，不嵌入供应商 URL；换引擎后来源链接仍有效。
- 页面不持有部署密钥；模型和模块选择只能来自服务器允许列表。
- 任务失败、审批拒绝、版本冲突、迁移维护和降级都有明确可操作状态。

验证对应 API-01、AUTH-01、PUB-01/02、DRAFT-01，并新增管理界面端到端场景。视觉稿和 UI 实现属于后续产品阶段。
