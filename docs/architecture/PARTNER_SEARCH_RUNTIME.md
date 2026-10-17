# 伙伴搜索 POST 接入

更新：2026-10-16。依据仓库 `openapi/partner.v1.json` 实现请求/响应映射，三版PG原生数据库、localhost HTTP及独立Schema复核已通过。

## 接口与应用边界

正式契约路径为 `POST /api/v1/knowledge-os/search`，由 `PartnerSearchHandler` 调用统一 SearchService。请求仅允许 query、space_id、kb_ids、top_k。身份来自临时 Bearer 凭据，request_id 由服务器生成；客户端不能提供 Principal、AccessScope、发布过滤器、模块标识或配置摘要。

请求按 JSON/UTF-8 和已声明字段解析，拒绝重复字段、字段别名、未知字段、非法类型、超限条数和重复知识库 ID。top_k 接受准确的整数值，包括 1.0；使用精确有理数判定，避免把接近整数的小数舍入成合法参数。请求体上限 1 MiB。

响应按契约输出 evidence、request_id、profile_version、profile_digest、timings、degraded。引用使用平台文档/修订/块/原件定位，供应商分数、内部检索实例和令牌不进入公开响应。当前没有静默降级，失败整体拒绝，因此成功响应 degraded 为空数组。返回 no-store。

## 知识库列表

- 省略 kb_ids：应用先认证，再从主库当前明确授权中列出该空间获准知识库，随后重新授权实际范围并执行查询。
- 显式空数组：保持无知识库请求，不查询候选；认证后、返回空结果前再次检查凭据。
- null、重复 ID 或非法值：拒绝。空列表绝不能解释为无限制访问。

PostgreSQL 目录实现以只读 Repeatable Read 事务检查当前发行方、主体、客户端及空间策略是否有效，并枚举该版本的明确 search 授权。无效身份/缺失策略返回失败；有效策略没有授权时可以返回正常空列表。目录结果不代替查询前和最终输出前的重新授权。

## 错误与启动

错误按 Error Schema 输出固定安全消息及关联 request_id。初次认证失败保留可识别的认证类别，HTTP 返回 401/unauthenticated 和 Bearer challenge，内部签名/撤销原因不外露。凭据副本在调用后清理。请求结构错误为 400，deadline 为 504，其余未分类应用失败保守返回 503；完整错误分类矩阵仍待补齐。

默认服务器已注册契约路径，尚未装配生产依赖时返回 503/capability_missing。测试宿主通过 newMuxWithSearch 或实际 HTTP 服务注入应用；这不代表正式生产 Bootstrap 已完成。临时 GET 入口继续独立存在，不能替代伙伴接口。

## 验证范围与缺口

定向测试覆盖严格 JSON、数值边界、公开响应、凭据清理、空列表/省略区别、目录后凭据撤销，以及未配置启动拒绝。

原生联调使用实际签名认证、数据库授权目录、已装配 PostgreSQL 检索及最终来源复核，经契约路径的 localhost HTTP 验证省略 KB、显式空数组和撤销令牌。独立 Python 按 SearchRequest/SearchResult/Error Schema 校验实际请求响应，并比对来源与配置摘要。

Schema 通过不表示所有有效取值和运行能力已全覆盖。当前应用知识库范围最多 256 个，单次原生 PG 检索仍是一库/关键词通道且查询字节预算更严格；完整多库目标规划、全部字段值域/错误分类、远程 Grant、网络发送屏障和生产部署验收仍开放。

其余伙伴 read/ask/feedback 接口、MCP、Vue、成熟混合检索和固定 Recall/性能验收仍待完成。

当前归档：505项输入，PG15/16/18各229项检查，全量Go/go vet、228项Python及三份OpenAPI检查通过。见[伙伴搜索运行证据](evidence/partner-search-runtime-review.json)。
