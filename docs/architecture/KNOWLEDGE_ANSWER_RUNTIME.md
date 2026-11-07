# 问答应用服务

`pkg/knowledge.AnswerService` 将认证、`knowledge.answer` 授权、已发布检索、有限证据选择、Generator 调用和生成后的再次检索复核串成一个应用边界。模型只能看到最多 5 条、总计 24 KiB 的已授权证据；生成结果中的引用必须来自这些证据，答案完成前会再次执行当前检索以发现撤权、撤回、删除或发布代次变化。

服务不实现反思轮次，`Reflection=true` 会拒绝，避免把尚未配置的追加检索误当成已支持能力。宿主通过 `KnowledgeAnswerApplication` 接入伙伴 REST 和 MCP；`memorys-server` 在设置 `DEEPSEEK_API_KEY` 后，以已加载的 profile version/digest 创建 DeepSeek Generator 并开放问答，否则保持 `capability_missing`。

流式 Generator 的 `meta` 和答案增量可以即时转发；引用和最终 `done` 事件只在生成后当前性复核通过时发出。模型失败、引用越界、配置摘要不匹配或复核失败都会返回安全的依赖错误。
