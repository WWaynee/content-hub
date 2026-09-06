# 检索质量评测集（P14）

本目录是 content-hub 检索质量评测基准的测试数据，配合 `../eval_test.go` 使用：

```
agent/retrieve/eval/testdata/
├── README.md                 # 本文件：标注标准 / 难度定义 / 句子索引规则
├── eval_queries.json         # 主评测集（20 条，easy 8 / medium 8 / hard 4）
├── eval_queries_sanity.json  # sanity 集（5 条：2 easy + 2 medium + 1 hard，开发期快速验证）
├── corpus/                   # 4 份评测语料（与 testdoc/2026招生工作/ 同源，复制入库保证自包含可复现）
└── doc_sentences/            # 4 份语料的「句子索引 → 文本」导出（供标注者对照）
```

## 语料与 doc_key

| 语料文件 | doc_key | 主题 |
|---------|---------|------|
| 报名条件与流程.md | `baoming_tiaojian` | 报名条件 / 随迁子女 / 不得报名 / 流程 / 材料 / 特殊类型报名 |
| 去年招生数据.md | `zhaosheng_shuju` | 报名人数 / 录取规模 / 考试日程 / 高中毕业生数据 |
| 录取规则.md | `luqu_guize` | 投档 / 批次 / 专业录取 / 加分 / 特殊类型 / 退档申诉 |
| 招生政策依据.md | `zhengce_yiju` | 法规依据 / 年度通知 / 可引用硬数据 / 政策小结 |

## 句子索引规则（重要，标注前必读）

`sentence_index` 是句子在**该文档入库句子序列**中的下标（从 0 开始）。入库序列的切分逻辑是：

1. `splitter.Split(text, 300)` 按「结构标题行分节 + 完整句切分 + 软上限封片」得到 chunks（标题行本身不是句子，不进序列）；
2. 对每个 chunk 的 `Content` 再调 `splitter.Sentences()` 切句；
3. 全部 chunk 的句子按 chunk 顺序拼接 = 入库序列（`doc_sentences` 表按 `(chunk_id, sentence_index)` 排出的顺序与此一致）。

**注意坑**：chunk 的 `Content` 是逐句无分隔符拼接的（`strings.Join(cur, "")`），
而 `splitter.Sentences()` 以句末标点（。！？；!?; 及换行）为界。因此一行以冒号「：」结尾、
下一行是分号/句号结尾列表项的文本会被**拼进同一句**（例如"考生报名时一般须提交以下材料：
1. 本人居民身份证原件及复印件；"因无句末标点分隔而成为一句）。
**不要**直接用 `splitter.Sentences(全文)` 对含换行的原文切句来数下标——那会把句子切得更碎、
与 `doc_sentences` 错位。正确做法是参照 `doc_sentences/<doc_key>.json`（已按上述入库序列导出），
或本地重跑 `TestExportSentenceIndexes` 重新导出。

## 标注标准

- 所有 query 必须是**自然语言问句**（不用关键词列表），覆盖"是什么/有哪些/是多少/怎么办/比较"等问法；
- 答案必须真实存在于 corpus 原句，不 paraphrase、不加工；
- `relevant` 里标注**所有**应该出现在检索结果前列的相关句；
- 难度判定：

| 难度 | 数量 | 定义 |
|------|------|------|
| easy | 8 | 关键词精确匹配即可找到，答案在同一段落内 |
| medium | 8 | 需要语义匹配或跨段落综合（query 用词与原文不同） |
| hard | 4 | 需要跨 2 份以上文档汇总，或答案隐含需要推理/计算 |

## 各字段说明

- `id`：q01–q20 唯一编号
- `query`：用户提问
- `difficulty`：easy / medium / hard（分布 8/8/4）
- `category`：按主题归类（数据·报名规模 / 报名·条件 / 录取·投档 / 加分政策 / 跨文档综合…），便于分类分析
- `relevant`：相关句子列表 `[{doc, sentence_index}]`
- `notes`：答案位置与标注理由，便于复核

## 验证命令

```bash
# 重新导出 doc_sentences（离线）
go test ./agent/retrieve/eval/ -run TestExportSentenceIndexes -v

# sanity 快速验证（5 条，开发期用）
RETRIEVAL_EVAL_SET=sanity go test -tags=integration ./agent/retrieve/eval/ -run TestRetrievalQuality -v -count=1

# 全量 20 条对比（需 MySQL/Qdrant/OSS + LLM/Embedding/Rerank key）
go test -tags=integration ./agent/retrieve/eval/ -run TestAllStrategiesComparison -v -count=1 -timeout 30m
```
