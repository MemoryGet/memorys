# 撤回命令语义与准备器

这是已实现并有跨语言字节对照的局部摘要规范；完整 V1 契约、远程协议、部署和生产授权仍未冻结。实现位于 [preparer.go](../../pkg/withdrawal/preparer.go)，金标位于 [semantic-vectors.json](../../pkg/withdrawal/testdata/semantic-vectors.json)。

## 编码与摘要

使用已有 AKOS-CJSON/1，UTF-8、对象键排序、无额外空白。Reason 按原字符串精确保留，不 trim，也不执行 Unicode NFC。ExpectedEpoch 编码为十进制字符串，避免跨语言浮点损失。

SHA-256 输入为 UTF-8(format + 单个 LF + canonical JSON)，输出小写64位十六进制。不能对 Go 结构直接 marshal 后取哈希，也不能改为另一种 JSON 规范化方式。

| 摘要 | 格式 / JSON字段 |
|---|---|
| ActorSemanticDigest | akos.withdraw.actor/1：format、subject_id、subject_type、issuer、client_id、delegation |
| CommandDigest | akos.withdraw.command/1：format、action、actor_digest、space_id、knowledge_base_id、document_id、expected_epoch、reason、profile_digest |

无委托时 delegation 为 null；有委托时只含 user_id、client_id，并要求其 client_id 与 Principal.ClientID 一致。SubjectType 仅允许 user/service。平台 ID 非空、UTF-8且至多200字符，Issuer至多4096字符，Reason非空且至多10000字符；ProfileDigest为64位小写十六进制。ExpectedEpoch必须大于0并允许后续递增，拒绝uint64最大值。

RequestID、ScopeID、PolicyVersion、ExpiresAt、Deadline、委托ProofReference和IdempotencyKey不进入命令语义。幂等键单独参与数据库唯一键；相同语义使用不同键仍是不同命令。临时字段可在重试中变化，但每次都必须重新验证当前身份、委托、权限与期限。Issuer、主体/委托身份、目标、旧epoch、原因或业务Profile变化则改变对应摘要。

## 准备器及信任边界

Preparer.Verify必须接入可信来源，核对完整Principal（含当前委托证明）、原Scope、document及当前业务权限。回调不可由客户端提供，不能仅以结构合法或两个对象相等代替身份/权限验证。缺少Verify、验证拒绝或验证后取消均拒绝准备，且不生成ID。

准备器取调用、Request及Scope期限的最小预算，给Verify独立请求拷贝，避免其修改原输入。验证后生成5个互不相同的平台ID（操作、历史、计划、事件、审计）；默认使用crypto/rand生成128位随机值，故障/重复ID拒绝。OccurredAt规范化为UTC，不参与命令摘要。生产可注入其他满足契约的ID/时间提供者。

Service直接使用Preparer.Prepare；数据库仍在各操作及提交前检查Scope。当前PG探针已经使用真实摘要/准备代码，身份验证、ID与时间来源仍是受控fixture，未证明真实IAM或跨服务即时撤销。

## 验证与后续

Go与独立Python对照两份固定金标，覆盖中文/换行、无委托、委托主体和uint64大整数。Go验证临时字段变化保持摘要、原因与Issuer变化改变摘要、授权失败/取消不分配ID、重复ID拒绝及请求拷贝隔离。真实PG应用探针核对该摘要下的正常提交、历史重放、摘要冲突及pending拒绝接管。

完整生产准入还需：真实Verify及提交前Scope来源、文档级授权公共测试、摘要格式随命令记录导出/迁移的显式元数据、配置版本冻结、原业务流程/迟到清理及完整WTX验收。当前仍不可注册完整PublicationService或metadata_uow。
