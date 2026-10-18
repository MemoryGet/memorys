# 运行时墙钟健康门禁

2026-10-18：将诊断阶段的墙钟/单调时间观测收敛为`pkg/clockhealth`可复用组件。它用于阻止新的配置搜索运行时在时间语义不可信时构造；不会修正系统时间、给租约增加宽限或替代数据库/认证器的时间判断。

`Tracker`以启动时UTC墙钟和单调耗时为基线，观察后续样本的相邻步变和累计偏差。普通调度停顿会同时增加墙钟和单调耗时，不会被判为跳变；墙钟相对单调计时超过阈值会将Observation标为Unstable。Observation只保存样本数、最大偏差和最大步变，外部没有权限据此改变业务期限。

`Monitor`提供有界间隔采样和幂等Stop，重复停止返回同一观察结果，并等待观测协程结束。`Check`对nil、已取消上下文、nil health回调或health失败全部fail-closed，并给外部健康回调施加1秒内部截止。生产Bootstrap应把健康实现绑定可信时间来源与节点同步策略，再把检查函数传给`knowledge.ConfiguredRuntime`或`ConfiguredReadRuntime`；当前默认`memorys-server`仍未装配数据库和生产依赖，因此继续返回503。

单元测试覆盖普通调度停顿、正负墙钟跳变、累计偏差、样本回收、重复Stop幂等性和零值/失败健康回调。组件本身不证明操作系统时间同步、数据库节点间偏差、TLS令牌时间或部署制品健康；这些仍是生产Bootstrap验收项。此前PG16实库运行捕获4至5秒跳变的失败证据继续保留在[GOVERNANCE_CLOCK_DIAGNOSTICS](GOVERNANCE_CLOCK_DIAGNOSTICS.md)。
