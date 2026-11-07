# 解析候选应用服务

CandidateService通过标准Parser与CandidateRepository衔接上层业务，不依赖具体解析器或数据库。宿主绑定不可变ProfileDigest，传入资源级Authorize回调。请求声明的配置必须匹配宿主绑定；调用方自行填写Principal/AccessScope不构成认证，回调必须验证当前身份、委托、文档/草稿/原件归属及读取和候选写入权限。

Prepare在有限调用预算及scope期限内执行：验证命令 → 当前资源授权 → 标准Parser → 候选来源/版本绑定 → 再授权 → 返回PreparedCandidate。供应商收到独立输入副本；其对scope/options的修改不能改变候选归属或后续授权。该阶段不打开元数据事务、不保存、不审批、不发布。

Commit重新授权并将冻结的完整候选交给CandidateRepository。仓储仍必须在自身事务内重新授权和CAS，应用服务的检查不能替代提交边界检查。返回回执必须匹配文档/草稿/候选ID；应用成功时版本应为预期版本加一，未应用时须有约定保留原因。

平台错误区分ErrCandidateRejected（确定没有提交效果）和ErrCandidateCommitUnknown（需要核对）。仓储返回未分类的网络/超时错误或不匹配的成功回执时，服务按提交结果未知处理。未知优先于拒绝，不向上层转发供应商细节。调用方必须保留同一个PreparedCandidate进行Commit重试，不能重新调用Prepare或分配新CandidateID，否则解析非确定性可能导致同ID不同内容冲突。

PreparedCandidate现在可通过 `CandidateService.EncodePreparedCandidate` 持久化为带格式和 SHA-256 封装的有界记录，再用 `DecodePreparedCandidate` 恢复同一解析输出；新增 `FilePreparedCandidateStore` 提供私有目录、原子写入、同候选幂等、冲突拒绝和删除实现。恢复不会延长原请求/scope期限，Commit 仍必须重新授权并由仓储执行 CAS。该接口提供跨重启候选重试的安全编码边界，但完整持久任务租约、恢复入口和后台调度仍需上层实现，生产API不能把编码接口单独当成完整任务系统。

验证包括解析前拒绝、解析后撤权、宿主配置/主体匹配、scope期限截断、解析器和回调修改隔离、未知提交不重新解析且保持原输出、成功回执错误和确定拒绝分类。真实本地文件/Markdown解析与本服务串联，候选仓储和权限是fixture。成熟Office/PDF独立解析探针、真实PG事务、当前IAM、HTTP/MCP和完整流水线配置归一化仍须分别完成验收。

后续真实串联已补充：文件/Markdown → 本应用服务 → PostgreSQL 16.15事务 → 冷连接读取与回执重放，含提交后丢失响应及人工编辑后的迟到候选。数据库和文件是真实实现，初始元数据与权限仍是fixture；见[串联证据范围](CANDIDATE_E2E_VALIDATION.md)。HTTP/MCP、审核发布、Office/PDF到数据库及跨重启恢复仍未完成。
