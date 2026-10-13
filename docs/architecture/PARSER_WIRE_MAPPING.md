# 解析模块进程内/远程字段映射与验证

本交付推进D-03的解析部分。实现见 [modulewire包](../../pkg/modulewire/parser.go)，机器契约来自modules.v1 OpenAPI。它是DTO编解码器，不执行解析、不发HTTP请求、不验证服务凭据或访问Grant。

## 1. 逐字段映射

| 进程内字段 | 远程字段 | 固定处理 |
|---|---|---|
| ParseRequest.Request.RequestID/Deadline/ProfileDigest | context.request_id/deadline/profile_digest | 请求追踪、UTC时间与不可变配置摘要 |
| ParseRequest.Scope的全部7字段 | context.scope对应字段 | scope_id、subject_id、action、space_id、kb_ids、policy_version、expires_at完整保留；不是授权证明 |
| 配置提供的Call.InvocationID/ModuleInstanceID | context.invocation_id/module_instance_id | 不从用户知识输入推导，contract_version固定1.0 |
| DraftID/Input/Format/Options | input.draft_id/input/format/options | BlobRef四字段；语言数组与OCR布尔显式映射，无下载URL |
| ParseResult.ParserVersion/Diagnostics | result.parser_version/diagnostics | 诊断code/safe_message/severity，不回显供应商异常 |
| ParsedBlock.ID/Order/Content | result.blocks的id/order/kind/text/source_anchor/structured_content | 将Content联合类型摊平，不输出Go字段名或虚构revision_id |
| SourceAnchor/OriginalAnchor | kind/blob_id/revision_block_id/locator/derived_from | original与revision互斥；派生来源保留，非法双标识拒绝 |
| Locator.Characters/Box | char_start+char_end/bbox | 成对码点范围；四元素左/上/右/下数组；其他页、工作表、段落字段保留 |
| Content.Table/Image/Audio | structured_content.type与相应载荷 | 恰好一个匹配kind；text没有结构化载荷；反向转换恢复正确指针 |
| 表格/媒体的全部字段 | 标准Table/Image/AudioContent字段 | 合并、公式文本、精确数字串、缺省/空字符串、媒体Blob及说话人标注保留 |

BlobRef实际包含id、sha256、size、media_type四个字段，映射全部覆盖；不包含文件路径或URL。结构映射表随Go类型字段增加而显式维护，字段数不匹配即失败，不静默忽略新字段。只注册平台DTO，不能将任意供应商SDK结构透传。

## 2. 精度与错误边界

- 整数使用json.Number及精确十进制转换；支持数学上相同的100、100.0、1e2，拒绝小数、溢出与负值到uint64的转换。policy_version大于2^53仍完整保存。其他语言实现必须使用无损数字处理，不能默认转double。
- float64字段先检查有限性，再核对其最短JSON数值表示与原数值的规范字节；无法无损往返的高精度或下溢输入拒绝，不静默舍入。需要更高精度的业务数值使用已有number_text等字符串字段。
- 时间规范为UTC RFC3339Nano：最多9位小数、没有冗余尾零；实际日历值验证，禁止截断更高精度时间。编码转换到UTC，进程内单调时钟信息不跨协议传递。
- 所有字符串须为合法UTF-8，拒绝重复JSON键、未知字段、未知联合类型、非法Unicode、尾随数据、显式null和缺失必需映射字段；缺省可选字段才恢复为零值或nil。
- 必需集合中的nil在编码时成为空数组，空KB数组仍表示空范围。可选指针区分缺省与显式空字符串；不自行补业务默认值。
- 响应request_id和invocation_id必须与调用预期一致。错误为安全通用信息，不回显输入值。

编解码器使用当前canonical参考实现的4 MiB输入/输出限制；离线检查包同样限制4 MiB。它们不代表生产解析器支持大载荷流式传输。大文档、网络响应大小和授权闸门仍须按各自门槛实现。

## 3. 现有证据

Go包测试覆盖请求往返、超过2^53的权限版本、等价整数字面量、四类内容与来源/范围/公式/数字串/说明/说话人字段、错误响应关联、未知字段、类型冲突、数值溢出、重复字段与非法UTF-8。

[check_parser_wire_mapping.py](check_parser_wire_mapping.py)用7项冻结金标构造远程解析请求/响应：先由独立Python Schema检查，再调用实际Go离线工具解码至contracts.ParseRequest/ParseResult并重新编码，最后再次检查Schema及规范字节。包含9位纳秒时间和大整数，也拒绝串用invocation_id。

```powershell
python docs/architecture/build_remote_contract.py --check
python docs/architecture/check_parser_wire_mapping.py
& '.\.tools\go\bin\go.exe' test -cover ./pkg/modulewire
& '.\.tools\go\bin\go.exe' vet ./...
```

[离线Go工具](../../cmd/architecture-parser-wire/main.go)只读取stdin中的request/response样例并输出重编码结果，没有生产网络入口。金标中的文件哈希和主体是测试值，不是真实解析或授权结果。

## 4. 尚未证明的能力

代码覆盖映射与部分格式约束，不是完整JSON Schema执行器，也不代替来源/原件/媒体真实性、合并区间、业务角色与当前授权检查。生产入口须运行完整Schema和领域验证；不能只调用DecodeParseRequest就信任Scope或调用解析器。

真实Parser运行、媒体提取Blob归属、迟到结果CAS、访问Grant、超时/取消及HTTP行为未验证。文件、嵌入、索引、检索、生成等端口的Go/wire对照也未完成。因此本交付只补齐解析数据映射证据，D-03完整验收仍保持未通过。
