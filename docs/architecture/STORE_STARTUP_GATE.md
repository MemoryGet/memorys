# 元数据数据源启动核对流程

本规范细化[数据源绑定](METADATA_STORE_BINDING.md)的真实启动路径。已有身份关系和codec只检查给定记录，未绑定观测到本次探测，不能排除旧观测重放。当前是设计交付；启动协调器、观测新鲜性字段和真实覆盖验证尚未实现。

## 1. 必需流程与就绪状态

```mermaid
flowchart LR
    A[可信构建锁与不可变配置] --> B[独立控制面当前绑定]
    B --> C[部署授权与探测调用身份]
    C --> D[真实读写端点观测]
    D --> E[身份与端点库存核对]
    E --> F[提交链覆盖验证]
    F --> G[再次核对当前绑定]
    G --> H[应用启动其余必需检查]
```

这只是全应用启动的一项必需门，不直接发布RuntimeSnapshot或将服务标为ready。权限、模块健康、凭据、后端索引布局及有效epoch仍须通过。失败或未知时停止接收知识请求；不得把缺失读端点作为可选降级。

流程状态为pending/probing/identity_checked/coverage_checked/current_binding_confirmed/failed。每个成功状态只说明本次阶段已经验证，不由清单自报布尔值推进。完成结果关联配置版本、控制面绑定版本及本次核对ID，不能被下一次重启无条件复用。

## 2. 期望来源和探测授权

从构建锁取得获准provider/实例/制品，再从独立控制面读取当前StoreBindingExpectation及可信版本锚点。期望不能来自正在核验的元数据库、模块settings复制值或供应商返回结果。读取与验证来源失败时终止。

探测使用DeploymentScope，动作拟定metadata.store.observe；覆盖验证使用metadata.store.verify_coverage。必须明确主体、控制域、资源范围、策略版本和有限有效期。用户知识查询Scope不允许调用这些控制面操作；探测接口不对伙伴MCP默认开放。

首版允许直接核对受控进程内端口；远程探测在接入前须另定义版本化协议、服务身份和请求绑定，不能将现有离线review包络直接发出并当作可信响应。日志、配置和返回值不包含连接凭据正文。

## 3. 本次探测的新鲜性

每个端点分配唯一probe_id和invocation_id，绑定本次check_id、父RequestID、期望绑定ID/版本/摘要、role、真实ModuleInstance及deadline。返回观测必须回显这些身份，并与经过认证的实际接收端点匹配。重试产生新调用身份，旧响应不能覆盖新尝试。

当前StoreProbeRequest只有Request/Scope/BindingID/Endpoint，StoreObservation没有RequestID/probe_id/期望摘要/时间界限。ObservationID唯一性检查只能防同一包内复用，不能证明本次调用新鲜。后续须版本化扩展并更新Schema、Go映射与兼容验证，不能默认为已有结构足够。

新鲜性也不来自“当前时间”字符串。观测应绑定请求挑战与接收链路，时间只限制允许窗口；即使字段全部匹配，未认证的响应或复制的数据库身份仍不能作为真实权威证明。身份/挑战异常是强制失败，不归类为传输可重试错误。

## 4. 覆盖验证及回执

对每个成功观测调用受控StoreCoverageVerifier，传入独立期望、实际观测、必需提交点及同一有限预算。验证器必须读取实际端点的封存日志/重放记录，核验stream、完整事务边界、前驱关系和权威模型覆盖；不能只比较RequiredCursor与ObservedCursor相同或字符串大小。

回执绑定本次check/probe/观测、端点、期望身份与摘要、所需/实际提交点、验证实现与结果引用。现有StoreCoverageReceipt只有观测ID/stream/cursor/reference，尚不足以表达全部新鲜性绑定；verification_reference非空不能代替真实验证。

没有真实链数据、覆盖未知或副本落后均阻止本门通过。是否允许等候追平由有界策略决定，计入总启动deadline；不无限轮询，不自动切到未知旧库。覆盖activation cursor不证明所有未来权限/撤回状态，查询期的当前状态检查仍必需。

## 5. 并发变化、失败与清理

全部端点验证后，再从独立控制面核对当前绑定仍与本次期望一致。版本、store epoch或端点变更则丢弃整个结果并有界重新开始；不把一个旧写观测和一个新读观测拼起来。控制面核对仍不是跨系统事务，具体部署需声明并验证其激活/接单线性化点。

启动协调器具有整体deadline及每端点子预算，观测数由明确端点库存限定。取消或故障时关闭探测连接和读观察点，未通过的工厂实例按内核生命周期回收。未知提交状态先查询激活事实，不能写入期望数据“修复”错误。

只有明确可重试的传输不可用或控制面版本冲突可以重新探测；拒绝、身份错配、模型不符、非法链及数据不完整均失败关闭。错误对外输出静态类别，详细诊断保存在受控审计中。失败不得修改权威数据或宣布迁移成功。

通过后仍须在维护切换、恢复和关键失效事件触发重新核对。若需要周期性验证，调度属于产品运维配置，本设计不自动创建任何定时任务。一次启动通过不是永久数据源凭据。

## 6. 实施验收

| 门槛 | 所需证据 |
|---|---|
| START-STORE-01 独立期望 | 当前控制面版本来源可信；从旧备份复制期望和旧绑定回退被拒绝 |
| START-STORE-02 观测绑定 | 真实端点与本次probe匹配；旧响应、错实例/端点/版本/调用及超期响应拒绝 |
| START-STORE-03 实际覆盖 | 同stream完整链、事务及重放覆盖；伪引用、字符串比较、部分事务不通过 |
| START-STORE-04 并发激活 | 探测期间切换绑定/模型/存储不会产生混合通过结果，明确接单边界 |
| START-STORE-05 故障清理 | deadline、拒绝、网络失败及重试，均有有限调用数和资源回收证据 |
| START-STORE-06 联合启动 | 本门与权限/健康/索引epoch等必需门共同控制应用ready，任何一项失败不接单 |

先升级探测/回执绑定契约，再完成类型化协调器和受控端点，最后开展真实数据库/控制面并发试验。上述门槛尚未执行；185项Python和Go局部检查不能关闭它们。STORE、AUTHORITY与整体目标保持开放。

## 7. 首批绑定探测Go契约

[store_probe_binding.go](../../pkg/contracts/store_probe_binding.go)新增独立StoreProbeBinding/BoundStoreProbeRequest/BoundStoreObservation及BoundStoreIdentityReader，旧离线StoreObservation格式保持。绑定包含check/probe/invocation/request、控制域/主体/Scope策略、绑定版本/期望摘要、RequestProfile摘要、目标端点及签发/截止时间；观测携带相同绑定与ObservedAt。

ValidateBoundStoreObservation先验证独立给定StoreBindingExpectation结构，再核对完整绑定、实际Request/部署Scope、目标端点/数据源身份和时间窗口。deadline为排他上界，观测不能早于本次IssuedAt或晚于可信now。时间使用精确time.Time比较，不把同一时刻的不同时区对象错误视为新探测。给定期望结构校验拆为ValidateStoreBindingExpectation，旧库存校验继续复用。

4项Go检查覆盖成功绑定/旧Probe与Invocation、端点/摘要/版本/存储错配、动作/域/策略/请求变动和纳秒窗口边界；全Go测试和vet通过，见[证据](evidence/store-probe-binding-review.json)。这些均是合成请求/观测，没有实际探测或认证。

期望摘要这里只检查格式及回显，不重新计算/认证独立期望；now由可信控制器提供，不能由响应方提供。相同请求的同一响应仍可能在有效期内重复校验，唯一Probe分配与接收账本必须在协调器中实现，不能把此函数称为完整防重放系统。Bound契约尚无Schema/Go wire映射、覆盖回执绑定或真实服务，START-STORE门槛仍开放。

## 8. 绑定探测离线机器契约

[store-probe-review.v1.schema.json](schemas/store-probe-review.v1.schema.json)定义独立expectation、request及observation闭集包络；复用已有数据源身份/端点模型，Probe绑定字段和部署Scope均闭集。它不是正式远程探测协议或签名载荷，不新增到现有17项远程操作清单。

时间采用issued_at_ns/deadline_ns/observed_at_ns/expires_at_ns正int64整数；策略与绑定版本为正uint64。Go端口仍使用time.Time，后续显式映射须检查纳秒表示范围并精确转换，不允许浮点、微秒截断或UnixNano溢出。原始JSON≤4MiB、文本≤4096 UTF-8字节，拒绝重复键、未知字段、非法Unicode/非整数值。

store_probe_validation.py按给定独立期望/请求检查Probe/Scope/端点/身份和纳秒窗口。now_ns由调用者提供的可信时钟观察值，而非响应正文；当前离线测试不证明这个时钟真实。报告binding_checked=true但authenticated/coverage_verified/ready_for_service固定false，未认证期望摘要或消费一次性账本。

5项新检查覆盖精确大整数、旧Probe/摘要/端点、权限/请求/数据源错配、纳秒截止/未来/请求前观测以及闭集/raw JSON/非整数时钟；全部190项Python回归通过，见[证据](evidence/store-probe-schema-review.json)。实际Go时间映射、真实服务、覆盖及接收账本未实现，START-STORE仍开放。

## 9. 绑定探测Go时间映射

`pkg/modulewire.Encode/DecodeBoundStoreProbeReview`显式映射Probe/Scope/请求/观测字段，将time.Time转为正int64纳秒并做往返同一时刻核对。范围外日期、零/负纳秒和UnixNano溢出拒绝；解码只接受整数token，不通过浮点/指数表示加载版本或时间。传入now也必须可精确表示。

离线cmd/architecture-store-probe接收单独now_ns参数及stdin review；该参数是测试输入，不是可信时钟证明。工具仅解码/关系核验/编码，不访问端点、不签发Grant、不消费Probe账本。它不加入既有远程17操作或伙伴MCP工具，未来正式协议仍须单独认证/版本验收。

3项Python检查调用实际Go工具验证独立Schema及完整JSON往返、纳秒/最大uint64、六类身份/窗口/类型错配及重复/别名/指数/尾随JSON拒绝。Go另验证正int64时间上下界、范围外年份和同一时刻不同时区。全部193项Python回归、全Go测试及vet通过，见[证据](evidence/store-probe-wire-review.json)。真实期望来源、时钟、服务认证、一次性账本、覆盖与启动协调器仍未实现，START-STORE保持开放。

探测一次性接收、控制器接管、提交未知与保留恢复细则见[探测账本](STORE_PROBE_LEDGER.md)。绑定校验不等于账本消费，accepted也不直接代表全应用ready。
