# 生产 HTTP Bootstrap

`pkg/bootstrap`是服务组合根：配置对象一次性接收搜索、当前读取、文档删除、任务状态、任务取消和MCP能力，并在注册路由前校验依赖关系。读取没有搜索身份源、删除没有Profile身份源时直接拒绝构造，避免半配置服务启动。

未提供某项能力时，路由仍存在但由对应handler返回503；Bootstrap不创建默认身份、不伪造授权、不把fixture提升为生产依赖。实际数据库、IAM、模块注册和后台Runner由宿主在构造`Config`前完成并负责生命周期。

需要后台事件消费时使用`bootstrap.NewRuntime`注入`events.Runner`；`Start`和`Stop`与HTTP组合根绑定，Stop会等待Runner退出并保留其最后投递错误。

`Runtime.Ready`在有Runner时要求其已启动、未停止且没有最近投递错误；没有后台Runner的纯HTTP组合保持可用。该状态用于部署探针，不替代业务任务状态或数据库健康检查。

`Runtime`实现`http.Handler`，`GET /ready`返回`{"status":"ready"}`，未就绪返回503，其他路径转发到组合根。

需要同时运行多个独立outbox消费者时使用`events.RunnerGroup`和`NewRuntimeWithRunnerGroup`。Group按稳定名称确定启动顺序，后续Runner启动失败会回滚已启动成员，停止按逆序执行；`Ready`只有在所有成员无最近错误时才通过。单Runner的`NewRuntime`保持兼容。

生产宿主可使用`LoadProductionSettings`校验监听地址、TLS数据库URL、HTTPS发行方、JWKS/Profile/模块目录等部署边界。实现推荐的`ProductionArtifactAssembler`时，`NewProductionRuntime`会先用`LoadProductionArtifacts`一次读取每个文件的完整字节，计算SHA-256并从同一字节快照解码，再把快照和`Digests`传给宿主；加载失败即拒绝启动。多消费者部署实现`ProductionRunnerGroupAssembler`并使用`NewProductionRuntimeWithRunnerGroup`，同一快照会交给所有命名Runner。基础`ProductionAssembler`仍保持兼容，但宿主必须自行完成同等制品绑定。需要实际内核装配时，`kernel.FactoryRegistry`按provider/release建立不可变factory目录，`profileassembly.BuildFromFilesWithRegistry`按模块目录和Profile阶段绑定、健康检查并构造Runtime。Assembler随后绑定PostgreSQL身份状态和数据库连接。生产构造器统一启动Runner/RunnerGroup和关闭assembler资源；Bootstrap不会创建fixture身份或在缺少assembler时合成业务依赖。当前`cmd/memorys-server`仍是未装配开发入口，生产数据库/IAM/Profile/模块目录需要宿主实现assembler后显式注入。

`NewProductionAuthenticator`提供推荐的身份绑定辅助：它把已校验的公钥、部署issuer/audience、显式subject type和token lifetime交给`identitytoken.Authenticator`，同时要求宿主注入真实数据库状态源；不会为subject type、过期时间或撤销状态提供默认值。

严格制品部署可使用`ProductionLockInput`和`NewProductionRuntimeWithBuildLock`：Bootstrap先调用`buildlock.VerifyFiles`核对部署/锁/保留策略/清单/制品的显式路径映射，失败时不会调用assembler；成功后把`ProductionLockedArtifacts`和离线报告传入宿主。报告仍不证明签名来源、实际运行制品、后端依赖、持久epoch或IAM健康。
