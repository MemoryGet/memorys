# 发布 Worker 宿主装配

`pkg/preparation.Runner` 只处理一次发布准备交付：领取租约、准备索引制品、用 fencing token 完成任务。`pkg/preparation.Service` 为它增加进程生命周期、停止、就绪和错误回调；它不伪造失败回执，也不把重试从持久任务队列搬到内存。

宿主可以选择：

- `postgres.NewPublicationService` + `postgres.PublicationQueue`，从 PostgreSQL 的 queued task head 发现任务；
- `preparation.NewRedisPublicationService` + `RedisList`，由宿主把 go-redis 等客户端包成两个最小方法。

两种方式都可以放进 `bootstrap.Config.PublicationWorker`。`bootstrap.Runtime` 会在 HTTP 启动前启动 Worker，在停止时先停止 Worker，并把 Worker 就绪状态纳入 `/ready`。队列负责发现、重试和死信策略；任务表的租约过期和 fencing 校验负责跨进程恢复。

当前 `localruntime.Search.NewPublicationWorker` 已提供 PostgreSQL 版本的 Runner/Prepare/Complete 装配，`publicationassembly` 可由 `memorys-server` 显式启用并绑定 Worker Bearer Token、space、KB 和 Worker 身份；Redis 客户端仍由宿主提供，进程重启、租约过期和真实索引效果仍需实库验收。
