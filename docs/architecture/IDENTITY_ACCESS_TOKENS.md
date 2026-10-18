# 访问令牌认证与当前状态

更新：2026-10-16。状态：已实现认证适配器，原生数据库链路已验证；不代表生产身份与权限系统验收通过。

## 模块边界

`pkg/identitytoken.Authenticator` 实现平台 `contracts.Authenticator`，仅产生 Principal。调用方继续通过独立 Authorizer 获得 AccessScope。令牌中的角色与 scope 不直接变成知识访问权限。

`StatusSource` 是可替换的当前状态接口；`pkg/postgres.IdentityStatus` 是默认数据库实现。每次认证重新查询主库中的发行方、签名公钥指纹、主体、客户端及令牌登记状态。任意对象停用、令牌撤销、指纹不一致或状态源失败均拒绝认证。它采用已登记令牌允许清单，需要可信发行流程登记令牌；不是对任意外部 OIDC 令牌的即插即用验证。

`identitytoken.LoadRS256PublicKeys`负责读取部署提供的JWKS快照并转换为独立的RSA公钥集合：文件大小、kid唯一性、RS256、签名用途、RSA位数和公钥/私钥边界均校验；不访问`x5u`/`jku`远程地址。宿主可在`ProductionAssembler`中加载该集合并构造Authenticator，密钥轮换通过替换封存快照和重建认证器完成。

查询复核控制器 `queryread.AuthenticatedControl` 在每次 Refresh 中重新执行认证、授权和当前事实读取。原生证据探针已改用真实 RS256 签名与数据库状态；发行方及初始策略数据由受控测试配置提供，证据主链路授权执行已改用数据库封存ACL，见[持久化授权](PERSISTENT_AUTHORIZATION.md)。

## 受限协议配置

依据 [RFC 9068](https://www.rfc-editor.org/rfc/rfc9068.html) 设计受限访问令牌配置：RS256、明确的 `at+jwt` / `application/at+jwt` 类型、可信配置中的 kid、公钥副本、匹配的 iss/aud，以及 sub/client_id/jti/iat/exp。nbf 可选。日期限定整数秒，不设置时钟宽限，最大有效期由部署配置指定且不得超过 24 小时。

使用已有 go-jose SDK 验签；严格检查重复 JSON 字段、紧凑编码、令牌类型、算法、密钥和日期边界。拒绝不受支持的 act/may_act/cnf，不通过远程头部 URL 获取密钥。普通 JWT 或模块调用 Grant 不能作为本模块访问令牌。

数据库存储 PKIX 公钥 DER 的 SHA-256 指纹，并匹配令牌登记的主体、客户端、签名键、签发及过期时间。kid 相同而密钥内容变化时，旧配置认证失败，需要可信配置更新。

## 验证与交付边界

单元测试覆盖签名篡改、类型与字段约束、受众与发行方、有效期、状态撤销、状态源不可用、配置复制及错误信息收敛。原生数据库探针覆盖实际签名认证、令牌撤销、主体/客户端/密钥停用、公钥指纹变化、未登记签名令牌及状态失败。运行证据必须与当前源码清单一致才可计为通过。

日志和报告不记录令牌原文或私钥；探针只输出令牌摘要、公钥摘要及测试身份关系。测试使用临时发行密钥。

尚缺生产发行方接入、密钥发现与轮换管理、用户及客户端管理、令牌登记/撤销管理和审计、持久化授权策略、配置装配、REST/MCP 入口集成。当前 SQL 位于隔离验证 schema，尚不是正式部署迁移。完整身份权限能力继续保持待验收。

本轮归档：[身份认证关系复核](evidence/identity-access-token-review.json)、[多版本矩阵](evidence/postgres-version-matrix.json)、[全量本地回归](evidence/unified-local-checks.json)。当前483项输入，三版PG各192项检查，Python228项；签名验证由Go SDK及定向测试执行，独立Python复核不重复进行密码学验签。

后续授权增量已将当前归档更新到485项输入/每版198项检查；前段483/192记录属于认证交付当时结果。生产身份管理与授权管理仍未完成。
