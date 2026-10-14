# Grant令牌及成熟验签器选型 V1

本规范承接[Grant绑定](ACCESS_GRANT.md)，决定首个实现候选采用紧凑JWS/JWT、专用令牌类型和go-jose/v4。已接入固定SDK并取得下述局部真实签名证据；完整身份/授权服务尚未完成。

## 1. 固定SDK候选

选择`github.com/go-jose/go-jose/v4` v4.1.5，解析commit为`b10771e937ddcce419e30fc0c2b9e38464a333d0`。LICENSE为Apache-2.0；go.mod要求Go1.24，当前平台Go1.26声明满足版本下限。SDK源码/许可摘要见[核验记录](evidence/go-jose-sdk-review.json)。Go模块版本与go.sum已锁定，并执行模块缓存校验；实际部署制品锁仍待完成。

[官方v4.1.5发布](https://github.com/go-jose/go-jose/releases/tag/v4.1.5)列出JSON深度、时间范围、曲线与JWK等修复。已查看[历史解析DoS公告](https://github.com/go-jose/go-jose/security/advisories/GHSA-c6gw-w398-hv78)，其v4受影响范围为4.0.5之前；不能据此宣称当前候选不存在其它漏洞。正式引入需核对完整适用公告并对实际构建运行漏洞检查。

SDK提供显式算法允许列表与验签API。解析不等于验签：调用Verify成功取得payload后再解码claims；禁止用未验证claims作为issuer/主体授权来源。[固定API](https://pkg.go.dev/github.com/go-jose/go-jose/v4@v4.1.5#JSONWebSignature.Verify)

## 2. 平台令牌Profile

| 项目 | 首版规则 |
|---|---|
| 序列化 | 仅紧凑JWS三段、一个受保护签名；不接收JWS JSON、多签名、detached payload或JWE |
| typ | 固定akos-access-grant+jwt，与用户OAuth Token/服务Token区分 |
| alg | 显式仅EdDSA，密钥为Ed25519公钥；不根据令牌算法动态扩大允许列表 |
| kid | 必须存在且匹配部署信任快照中的唯一公钥；不是下载URL或路径 |
| 密钥来源 | 受控配置的固定issuer→公钥集合，不使用令牌jku/x5u/jwk等自带信任源 |
| header | 只允许alg/typ/kid；拒绝未知crit、b64扩展及其它非Profile字段 |
| 大小 | token最多16KiB，受保护header最多2KiB，payload最多8KiB；解码前后均检查 |
| 载荷 | 下述封闭claims；唯一字段、精确整数、非法UTF-8/重复JSON键拒绝 |

这是平台策略选择，不表示库自动执行全部规则。边界检查和key selection由薄适配器负责；签名计算/验证由成熟SDK实现，不自行实现JWS密码学。若运行环境要求其它算法，新增显式Profile并按同一契约验收，不自动协商。

平台设计遵循[JWT最佳实践](https://www.rfc-editor.org/rfc/rfc8725.html)中的算法允许列表、受信密钥/issuer、audience和令牌类型隔离原则。具体字节预算、Ed25519选择及封闭字段是本平台决定。

## 3. Claims及纳秒时间

载荷字段限定`iss/sub/aud/jti/iat/nbf/exp/akos`。iss绑定受信issuer，sub是原主体，aud必须为单个字符串且等于目标模块实例，jti是唯一Grant令牌ID。akos为现有Bindings对象，重复身份/时间须按规则一致，不能在两个位置使用不同issuer或subject。

标准iat/nbf/exp为精确整数Unix秒；现有akos字段保留精确Unix纳秒。iat=floor(issued_at_ns/10^9)，nbf=floor(not_before_ns/10^9)，exp=ceil(expires_at_ns/10^9)，适配器核对这三个关系，随后执行原有精确纳秒边界。粗粒度exp只作外层范围，不能使令牌在纳秒deadline后继续有效。零偏差是首版基线；不采用SDK缺省leeway延长平台期限。[JWT时间语义](https://www.rfc-editor.org/rfc/rfc7519.html#section-4.1.4)

签发器不改写请求deadline，Bindings与method/path、Scope及请求摘要固定一致。合法重试可签发新jti但仍绑定同invocation与原请求；重放/幂等由持久调用账本控制，jti唯一不等于自动实现业务去重。

独立Claims JSON Schema尚未提供；Go适配器已有封闭字段/精确整数及标准秒与纳秒一致性检查，内部Bindings Schema仍不能直接校验完整JWT载荷。签名以实际JWS字节处理，不以AKOS-CJSON重新序列化后验签；平台规范编码只用于请求/Scope摘要和验证payload字段唯一性。

## 4. 密钥轮换及故障

信任快照含issuer、kid、算法、用途、受控公钥引用、启用/停止签发时间、验证截止时间、版本与摘要。签发服务只持有当前签发私钥，模块只有公钥；私钥放凭据服务，不进入普通配置、日志或构建锁正文。

正常轮换先分发新公钥并核对各服务信任快照，再启用新kid签发；旧公钥至少保留覆盖旧令牌有效期和约定在途窗口，之后关闭验证。未知/重复kid、配置无效或快照过期拒绝，不能临时尝试全部密钥。紧急移除受损kid停止新验签，权限撤销仍经当前访问闸门；已经通过检查的在途字节不能追回。

首版采用受控快照与重启分发，不实现按令牌URL自动获取JWKS。未来远程JWKS接入须限定地址、超时、重定向、缓存期限及轮换同步，仍通过KeyResolver接口转换。SDK、KeyResolver、服务身份验证和当前Authorizer分别替换，领域DTO不暴露jose库类型。

## 5. 正式接入门槛

| 编号 | 运行验收 |
|---|---|
| TOKEN-01 | SDK签发/验签正确；篡改header/payload/signature全部拒绝 |
| TOKEN-02 | none/HS256/其它曲线、错typ/audience/issuer/kid、重复键及超限拒绝 |
| TOKEN-03 | 秒/纳秒边界、超范围、未来签发、deadline后验证拒绝 |
| TOKEN-04 | 新旧kid重叠、旧key截止、紧急移除及未知kid失败封闭 |
| TOKEN-05 | 已签Grant的当前撤权/撤回、伪造服务主体、父任务越界拒绝 |
| TOKEN-06 | 同invocation重放与新jti、控制目标和结果未知的账本联调 |

SDK局部签名/边界证据见下一节；TOKEN-01至06的全部组合及真实授权仍未通过，D-03保持开放。

## 6. 首个SDK适配器及局部运行证据

`pkg/accessgrant.Verify`接入go-jose/v4 v4.1.5，领域输入输出为平台类型，不暴露jose SDK。调用者提供独立Expectation、不可变公钥Trust快照和精确当前纳秒时间；验证不按令牌issuer/kid派生网络地址。薄适配器限制紧凑三段、解码预算、EdDSA/header字段，SDK负责密码学验签，随后检查封闭payload、字段存在/大小写、精确整数、外层/内部身份及时间关系和完整调用绑定。

所有失败返回静态ErrInvalid和空Claims，不回显令牌、原异常或供应商文案。成功只返回signature-verified/request-bound Claims，不代表调用服务已认证或当前资源访问已授权。调用者仍须执行operation策略、可信父任务、当前权限/发布及持久账本；不将Verifier独立用作业务Authorizer。

4项Go测试使用SDK真实签发的临时Ed25519密钥，覆盖正确结果、header/payload/signature篡改、有效签名但错身份/动作/摘要/audience、错typ/kid、移除公钥、纳秒截止前后一单位、未来not-before、秒关系矛盾、缺/重复/大小写字段、float/null整数、超限及HS256算法混淆。见[执行证据](evidence/grant-sdk-runtime-review.json)。这些测试中的Trust、Expected及时间是合成输入，不证明生产凭据或授权来源。

首版底层测试验证移除kid；随后第7节新增窗口与不可变快照测试。新旧快照分发、并发刷新与紧急轮换仍没有服务级证据。没有完整漏洞扫描、配置工厂装配、TLS/服务身份、当前撤权、父任务/重放/结果未知账本或17操作端到端Grant联调；正式接入仍未通过。

## 7. 不可变公钥快照与窗口

`pkg/accessgrant.NewVerifier(SnapshotConfig)`生成私有Verifier实例，复制每个公钥字节与记录，不保留调用方可写map/slice。校验issuer/ID、非零版本、快照有限区间、最长Grant预算、1至1000个唯一kid、EdDSA/32字节公钥及enabled<sign_until≤verify_until。配置不合法返回静态ErrInvalid，不分配可用实例。Go直接Trust入口作为底层验证辅助器保留，正式工厂应使用不可变Verifier，不向业务层暴露可写Trust。

每次Verify要求now位于快照有效区间及该key验证区间，令牌issued_at处于该key签发区间，expires_at不晚于该key验证截止；区间均左闭右开。提前关闭验证窗口会拒绝越过窗口的令牌，不能只凭当前时刻尚未截止就接受超长有效期。它不验证签发服务是否真的遵守签发截止；拒绝违反窗口的已签令牌是接收端守卫。

SnapshotIdentity返回ID/version/digest。摘要用域akos.key-snapshot/1＋NUL＋平台规范编码的公钥配置；keys按kid排序，公钥采用Go JSON的标准Base64字符串，整数精确保留。版本、窗口、issuer、预算及所有公钥均被绑定；无私钥或凭据正文。该摘要随后第8节已有独立机器Schema和Go/Python金标；可信分发签名及持久回滚防护未实现，不能自证配置来源。

新增4项真实SDK签名测试覆盖新旧kid重叠、调用方修改公钥/配置后不影响实例、签发截止、超出验证期限、快照到期、重复kid/错误算法/长度/窗口、移除后新实例拒绝旧key及摘要顺序/版本绑定。见[执行证据](evidence/grant-key-snapshot-review.json)。

旧Verifier仍保持原快照；部署协调器须将新实例与持久版本/BuildLock核对后替换，并确认所有进程采用该代际。构造器不自动比较历史版本，也不广播撤销。当前没有运行注册工厂、分发/回滚防护、签发器轮换调度、并发刷新或生产身份/当前授权，TOKEN-04仅取得局部基础证据，D-03未关闭。

## 8. 严格JSON加载与独立部署期望

[公钥快照Schema](schemas/key-snapshot.v1.schema.json)与[独立金标](examples/key-snapshot-golden.v1.json)由build_key_snapshot.py生成。公钥仅接受32字节的规范标准Base64字符串，不接受数字数组、URL或JWK额外字段。字段闭集、时间int64、版本uint64、key数量明确；窗口/唯一kid/规范Base64低位和标识UTF-8字节预算另由语义检查执行。JSON Schema的maxLength按码点；Go/Python语义层都限制标识最多200 UTF-8字节。

`LoadVerifier(raw, expected SnapshotIdentity)`先拒绝重复/缺失/大小写替代字段、null/float整数、未知供应商字段和非规范key编码，再调用不可变构造器，并要求生成ID/version/digest与独立部署期望完全一致。成功不代表期望来自可信控制面，不自动比较历史高水位。原始JSON不触发路径解析、下载或网络访问。

Go使用现有4MiB/64层规范JSON预算，key最多1000条。Python参考保留相同精确编码，按kid排序后计算独立摘要；金标version=9007199254740993验证不经double，另含int64上限和零纳秒。金标公钥为合成配置字节，未用作真实签名或可信身份。

新增Go3项、Python4项检查覆盖规范加载与跨语言摘要、独立期望错配、格式/时间/重复字段、供应商URL、非字符串及非规范Base64、版本/窗口摘要变化。见[执行证据](evidence/key-snapshot-load-review.json)。此处只证明输入和独立期望一致；配置分发签名、控制面资格、持久高水位、工厂装配及所有进程切换仍未验，D-03/D-04保持开放。

```powershell
python docs/architecture/build_key_snapshot.py
& '.tools/go/bin/go.exe' test -count=1 ./pkg/accessgrant
```

## 公钥激活、版本高水位和历史恢复（2026-10-13）

详见[信任激活协议](KEY_TRUST_ACTIVATION.md)。记录TrustDomain/Head、不可变快照、KeyRetirement与激活操作的归属，禁止低版本回退及受损公钥换kid复活；历史恢复必须核对备份之外的受信控制面锚点。现有仓储/BuildLock尚未包含完整机器绑定，也无持久激活/分发证据，后续须补齐。
