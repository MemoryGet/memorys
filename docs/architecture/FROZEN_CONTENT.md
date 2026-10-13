# 内容冻结、规范编码与哈希 V1

本规范解决验收问题D-02，定义唯一的哈希输入字节。格式标识为`akos.freeze/1`，编码为平台`AKOS-CJSON/1`，不是RFC8785/JCS，也不是任意JSON库默认输出。Go参考实现见 [canonical包](../../pkg/canonical/json.go)，独立Python实现见 [canonical_content.py](canonical_content.py)。

## 1. 冻结载荷与字段选择

机器契约见 [frozen-content Schema](schemas/frozen-content.v1.schema.json)，从标准内容Schema生成，仅包含以下根字段：

| 字段 | 固定规则 |
|---|---|
| format | 固定字符串akos.freeze/1 |
| blocks | 非空有序FrozenBlock数组；每个对象包含id、order、kind、text、source_anchor及类型要求的structured_content |
| blobs | 当前Revision的全部原件/媒体附件BlobRef，按id的Unicode标量顺序排列，id唯一；包含id/sha256/size/media_type |

FrozenBlock使用标准Block载荷，去除revision_id，增加order。数组位置就是order（0至n-1），Block.id唯一。内容、结构、来源和Block.id参与哈希。Revision ID、Document ID、父版本、创建人/时间、mutable title、任务/审批/发布状态、供应商标识和物理存储位置不进入载荷；它们仍按自己的领域规则保存，不能被丢弃。

冻结映射是白名单选择，不是在任意业务对象上自动删除几个字段。实际Frozen载荷中出现未知字段即拒绝。引用里的逻辑Block/Blob标识保留；同一版本的索引重建、供应商ID或文件位置变化不改变哈希，新Block ID或来源变化可以改变哈希。

检索投影若需要标题，使用冻结Block中的标题内容；不能使用可变Document.title悄然改写已批准投影。目录标题仅作展示元数据，涉及权威内容的修改继续产生新Revision并审核。

表格cells按row、column排序且坐标唯一；合并关系和精确number_text保留。音频segments按start_ms、end_ms稳定排序，完全相同时间保留原相对顺序，允许重叠语音。derived_from、paragraph_path及所有其他数组保持定义好的输入顺序。冻结前必须完成这些域内排序/约束，编码器不推测业务顺序。

字段缺省、空字符串、空数组和null不自动合并。仅在Schema允许时接受null；当前结构化内容的可选说明字段省略表示缺省，显式空字符串是不同值，null会被拒绝。不得自动补默认值、改写Unicode、换行或空白后再假称原内容哈希。

## 2. AKOS-CJSON/1精确编码

输入是无BOM的有效UTF-8 JSON。对象键解码后必须唯一，包括转义形成的同名键；拒绝无配对代理码点、非有限数值、尾随数据。文本按Unicode标量保留，不做NFC/NFKC、大小写或换行归一化。

输出规则：

1. 无BOM、无缩进、无多余空白、无末尾换行。
2. 对象键按Unicode标量字典序排列；合法UTF-8字节序与此一致。不是UTF-16排序，不使用语言/地区排序规则。
3. 数组保持顺序。布尔与null编码为true/false/null。
4. 字符串使用双引号，双引号与反斜杠分别转义；U+0008/0009/000A/000C/000D使用\\b、\\t、\\n、\\f、\\r；其他U+0000至001F使用小写四位\\u00xx。其余合法标量直接UTF-8输出，包括中文、emoji、U+2028/U+2029、/、<、>、&。
5. 数值按十进制词法精确处理，不能先转float64。将整数及小数部分连接为十进制系数C，E等于原指数减小数位数；去系数前导零，再去尾零并相应增加E。非零输出为可选负号＋无首尾零的C＋小写e＋十进制E；E不含正号或前导零。所有正负零输出单字符0。

例：`10`与`1e1`均编码为`1e1`；`1.2500`编码为`125e-2`；`0.001200`编码为`12e-4`；`-0.0`编码为`0`。字符串`"0.00000001"`仍为字符串，不能按数值重写。不同数值词法表示相同十进制数时哈希一致。

数值词法最多1024字节、原系数最多256位、原指数和非零归一化指数均在[-10000,10000]；容器嵌套最多64层。当前参考函数另外限制输入和输出各4 MiB，超限返回明确错误，不截断内容。该运行限额不是平台原件或整份文档的统一上限；生产大文档需要流式实现同样的规范字节，并验证分段处理与一次编码完全相同。不能将当前参考实现视为大文档能力已完成。

## 3. 两级哈希与版本

使用SHA-256，保存小写64位十六进制。C(x)表示上述规范字节，`||`表示字节连接，前缀末尾是ASCII LF，其他部分无分隔空白：

```text
blocks_hash = SHA256( UTF8("AKOS-BLOCKS/1\n") || C(blocks) )

header = {
  "format": "akos.freeze/1",
  "blocks_hash": hex(blocks_hash),
  "blobs": blobs
}

content_hash = SHA256( UTF8("AKOS-CONTENT/1\n") || C(header) )
```

冻结载荷自身format在计算前验证。块哈希覆盖正文、表格/媒体、来源、Block标识及顺序；内容哈希进一步覆盖全部原件/附件清单，不能只核对正文。原件字节SHA-256在BlobRef中保存，来源或媒体引用必须在清单内，媒体BlobRef与清单逐字段一致。

Revision保存HashFormat、BlocksHash、ContentHash和不可变内容。审批绑定Revision ID与ContentHash，不能因另一Revision具有相同哈希就复用批准。导出同时保存格式号与哈希；遇到未知格式失败，不能套当前算法重新解释。编码、字段或哈希范围发生破坏性变化必须使用新格式及新前缀，旧版本按旧算法核验，不能原地重算历史审批哈希。

## 4. 验证责任与接口映射

冻结应用服务先验证草稿归属/CAS、标准Schema、内容结构、来源范围、原件字节和附件归属；产生上述白名单载荷并验证排序与引用；再计算哈希，最后在同一个权威事务中保存Revision/Block、审批请求及审计/outbox。

Go `canonical.Frozen`验证编码及根包络，计算两级哈希，**不替代Schema和领域验证**。Python `check_frozen_content`额外验证结构、顺序及Blob清单引用，也不验证真实文件、权限、媒体尺寸/时长或表格合并覆盖。不能使用哈希存在作为允许审核/发布的充分条件。

管理Revision响应包含hash_format、blocks_hash、content_hash、blobs及标准blocks。客户端若需复核，将blocks去revision_id并按响应顺序增加order，连同排序的blobs构成冻结包；不把创建时间、主体或供应商字段加入。正式传输映射和提交事务仍待实现。

## 5. 金标及复核

[7项金标载荷](examples/frozen-content-golden.v1.json)保存输入JSON、精确规范JSON、两级哈希。覆盖中文/emoji及原件位置、人工修订/派生来源、LF与CRLF、合并表格与精确数字串、说明缺省与空字符串、图片与重叠音频。金标由Python参考生成并由独立Go编码/哈希实现交叉验证，另有手写原子编码断言；这不是外部机构认证。

6项Python测试验证原子字节、转义/数值/对象序一致、换行/空值差异、附件变更、Schema/顺序/清单拒绝，以及非法Unicode/数字/深度。Go另验证全部金标、错误包络、容量限制和合法代理对。

```powershell
python docs/architecture/build_frozen_content_contract.py --check
python docs/architecture/build_frozen_golden.py
python docs/architecture/check_frozen_content.py
python -m unittest discover -s docs/architecture -p 'test_*.py'
& '.\.tools\go\bin\go.exe' test -cover ./...
```

正常验证不重写金标。规范有意变更时先审查版本兼容性，再显式使用`build_frozen_golden.py --write`生成候选向量；历史格式的向量不得被静默覆盖。HashFormat=akos.freeze/1尚未投产，冻结前仍须补足真实提交、序列化映射与大载荷试验。
