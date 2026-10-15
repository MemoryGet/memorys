# 解析候选与草稿合并边界

解析器输出不再需要立即改写草稿。`Pipeline.ProcessCandidate`将标准ParseResult转换成携带DraftID、ExpectedVersion、原件完整BlobRef、ProfileDigest、ParserVersion、候选块和诊断的ParseCandidate。它不创建Revision，也不批准或发布。

`document.MergeParsedCandidate`是仓储事务可复用的纯领域规则：

- 同一草稿、仍处于draft状态、row_version匹配且原件BlobRef未变化，才生成拟保存的新草稿。
- 用户修改造成版本变化、原件替换或草稿已提交时，返回Applied=false、明确原因和完整候选，保持草稿内容。
- 成功时草稿版本递增。新块从版本1开始；复用块ID则从当前块版本递增，拒绝溢出，避免编辑CAS发生版本回退。
- 草稿、候选和调用方输入使用独立内容副本。修改拟保存草稿不会污染保留的候选或解析结果。
- 当前实现接受单原件解析契约，拒绝外部原件、伪造Revision来源、非顺序候选块和带持久化行版本的解析块。多原件派生输出需要扩展契约后再接入。
- 草稿含多个原件时返回multiple_originals并保留候选，不用单原件输出替换整份草稿；多附件的部分合并需要显式块归属与增量策略。

这不是数据库提交操作。宿主必须在可信元数据事务中重新读取草稿和权限、调用规则并执行SaveDraft的expectedVersion CAS，同时保存独立候选、审计、outbox和任务回执。事务CAS失败时仍需持久保留候选；不能把本函数Applied=true当作已保存，也不能在函数外绕过并发检查。隔离PG的同步候选事务已在WSL PostgreSQL 16.15运行验证，完整任务/审核事务及生产端点未完成，不能以此宣称可上线。

测试覆盖人工编辑、附件替换、提交后迟到结果、配置/来源绑定、块版本递增及副本隔离。真实Office/PDF探针将成熟解析器输出送入相同合并规则，检查可应用与迟到保留两种结果；草稿仍是内存fixture，真实IAM、持久化候选、审核CAS和任务绑定保持开放。

后续增量：[同步候选持久化接口](CANDIDATE_PERSISTENCE.md)已接入隔离PG适配器；其具体运行验证以数据库报告为准。这里的纯函数与实际解析器探针仍不产生持久提交回执，完整Metadata UoW、后台任务租约回执和审核状态机仍未完成。
