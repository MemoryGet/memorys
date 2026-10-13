# 标准内容载荷与来源格式 V1

类型定义统一保存在 [伙伴OpenAPI components/schemas](openapi/partner.v1.json)，内部内容仓储和解析器DTO使用相同语义。存储替换时可以使用不同物理结构，但导出/导入必须恢复这些载荷。解析结果不是厂商对象的透传。

## 1. Block 的两种表示

每个Block有kind、text和source_anchor。text是用于检索的文字投影；table/image/audio还必须含对应structured_content。两者一起冻结并计算内容哈希。修改结构化内容时必须重新生成或人工确认文字投影，不能更新表格后仍用旧text发布。

text类型不能夹带结构化载荷。多种内容混排分成有序Block，以共同的Revision保存。图片说明、OCR和音频转写属于可审核内容，不自动视为原件中的确认事实。

内容冻结的白名单字段、数组顺序、缺省/空值、Unicode和精确数值编码见 [冻结规范](FROZEN_CONTENT.md)。普通JSON库默认序列化不能直接作为审批内容哈希。

## 2. 表格

TableContent保存row_count、column_count和稀疏cells。行列从0计数；一个合并单元格用左上角位置和row_span/column_span表示，不复制成多个独立事实。role区分数据、列头、行头和交叉表头。

单元格text保持可展示的原始内容；number_text可保存精确数字串，避免浮点转换丢失精度或单位。formula仅作原始公式文本，不执行宏、公式或外部链接。额外业务数值解析进入新的受版本管理字段，不能让供应商私有类型进入核心单元格模型。

缺失cell表示未知或没有解析到；显式text=""表示已确认的空白。表格文字投影应带表头、单位和合并关系，使检索片段的含义可解释。不能为了统一解析把所有表格只保存成一段Markdown。

语义检查必须保证：单元格及合并区在边界内、无重复坐标和重叠覆盖、所有cell来源属于当前Revision或其合法原件、文字投影与结构版本一致。巨大跨度不能展开成无限网格；使用区间检查并限制总单元格/文本字节和解析预算。

## 3. 图片与音频

ImageContent保存逻辑BlobRef、可选成对width/height、caption和ocr_text；没有外部public_url字段。BlobRef含平台id、sha256、size和media_type，读取通过平台授权代理，文件路径不出现在产品接口。

AudioContent保存BlobRef、duration_ms和segments。时间采用毫秒半开区间；起止必须满足0≤start≤end≤duration。重叠语音允许时间区间交叠，不能用“一律不重叠”删除多人同时说话内容。segments按起点、终点和原顺序稳定排序，speaker_label只是标注，不是经过认证的用户身份。

媒体语义检查还需核验Blob归属、字节哈希、真实格式与声明media_type、图像尺寸和音频时长。只通过JSON Schema不能证明这些值真实。

## 4. 来源定位

SourceAnchor分original和revision：原件类型要求blob_id；人工修订要求revision_block_id，原件只作为derived_from。不得用原件页码暗示人工改写是逐字原文。

Locator提供PDF页码、幻灯片号、工作表/单元格范围、段落路径、Unicode码点范围以及bbox。bbox是[左,上,右,下]，原点在规范显示页左上，坐标归一化到0–1；必须有page并记录稳定的显示方向。页旋转转换由适配器完成，不能让不同解析器使用不同坐标系。

char_start/char_end同时提供并采用半开区间，必须在对应规范内容范围内；cell_range必须提供worksheet。没有可靠位置时precision=coarse并显示粗粒度来源。精确位置不允许由生成模型猜测；source范围与citation.block_id必须经过平台校验。

## 5. 替换验收

解析器A/B处理同样的合并表格、中文/emoji、旋转扫描PDF、图片OCR和多人音频；比较结构完整性、来源对齐、哈希及文字投影。解析差异会改变权威内容时，形成新Revision并审核。

文件与检索引擎替换不能改变标准载荷或公开citation。索引只消费投影；读取和审核页面始终从平台内容仓储取得结构化原文。回退演练必须覆盖表格合并关系、媒体Blob和人工修订来源，不能只验证纯文本。

运行 `python docs/architecture/check_partner_contract.py` 校验结构样例。语义、媒体真实性、解析保真和跨模块读取仍待真实集成测试，不能把结构样例通过算作完成多格式支持。
