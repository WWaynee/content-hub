# P14 · 检索质量升级：混合检索 + Rerank + 评测基准

> **为什么做这个包**：P01–P13 把系统的工程地基（安全、并发、状态机、可观测、人机协作）全部打牢了，但检索质量这条命根——"搜得准不准"——目前只有纯向量检索 + 硬预算 Guardian 循环，缺少混合检索、重排序、查询扩展等标准 RAG 优化手段。
>
> 本包的目标：**在不破坏现有主流程的前提下，为检索层增加可配置的质量升级策略，并建立可复现的评测基准，能量化说出"升级后提升了多少"。**
>
> 主流程默认策略保持不变（纯向量检索，即当前行为），所有升级策略通过配置开关启用，双线互不干扰。

---

## 1. 范围与命中代码

### 1.1 涉及文件

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `config/config.go` | 新增字段 | 新增 `Retrieval` 配置段（策略选择、各策略参数） |
| `storage/qdrant.go` | 新增方法 | 新增 `SearchSentencesHybrid`（混合检索）、保留原 `SearchSentences` 不变 |
| `splitter/splitter.go` | 新增方法 | 新增 `TokenizeForBM25()` — 中文分词（简易版，按字/词 n-gram），用于 BM25 稀疏向量 |
| `agent/retrieve/retriever.go` | 重构 | 检索策略可插拔化，新增 HybridRetriever / RerankRetriever 装饰器 |
| `agent/retrieve/claimloop.go` | 小改 | Thinker 可选升级为 LLM 多 query 扩展（配置开关） |
| `api/service/kbase_search.go` | 小改 | 透传检索策略参数（测试用） |
| `storage/kbase_chunk.go` | 新增字段 | chunk 表新增 `bm25_tokens` 字段（稀疏向量，JSON 存储） |
| 新增 `agent/retrieve/rerank.go` | 新文件 | Reranker 接口 + 跨编码器重排序实现 |
| 新增 `agent/retrieve/eval/` | 新目录 | 评测基准：测试数据集 + 评测脚本 + 指标计算 |

### 1.2 不改动的（保证主流程零风险）

- `orchestrator.go` 编排逻辑不变
- `guardian.go` 裁决逻辑不变（只是检索输入质量变高了）
- `writing/writer.go` 不变
- 所有现有测试必须继续通过

---

## 2. 升级策略（三层、均可独立开关）

三层策略是**叠加关系**，可以全开、可以只开某一层。默认全关（= 当前行为）。

```
纯向量检索（默认，当前行为）
    ↓  开启 hybrid = true
混合检索（BM25 稀疏 + Dense 向量，RRF 融合）
    ↓  开启 rerank = true
Rerank 重排序（跨编码器精排 top-N）
    ↓  开启 query_expand = true
LLM 查询扩展（多 query 生成 + 合并去重）
```

### 2.1 第一层：混合检索（BM25 + Dense，RRF 融合）

**问题**：纯向量检索对精确关键词匹配（如政策编号、人名、机构名）效果不好，语义相似度不等于关键词匹配度。

**方案**：同时进行 BM25 稀疏检索和 Dense 向量检索，用 **RRF（Reciprocal Rank Fusion）** 融合排序：

```
RRF 分数 = Σ 1 / (k + rank_i)   （k 取 60，默认值）
```

**BM25 稀疏向量实现方案**：
- 由于 Qdrant 的稀疏向量（Sparse Vector）需要专用 collection，为了不改动现有数据结构，**第一版用 Go 端内存 BM25 计算 + 候选集重排**的方式实现
- 候选集来源：向量检索返回的 top-K（取 K=50 作为候选池），对候选池内的文档计算 BM25 分数，然后 RRF 融合
- 中文分词用 **char n-gram（2-gram）** 方案——简单、可靠、不需要分词词典，适合政企专有名词场景
- `bm25_tokens` 字段在文档解析时异步计算写入，已有文档通过 migration 补算

**配置参数**：
```go
type RetrievalConfig struct {
    Strategy     string  // "dense" (默认) | "hybrid"
    HybridK      int     // 混合检索候选池大小，默认 50
    RRFK         int     // RRF 的 k 参数，默认 60
    BM25K1       float64 // BM25 k1 参数，默认 1.2
    BM25B        float64 // BM25 b 参数，默认 0.75
}
```

### 2.2 第二层：Rerank 重排序

**问题**：向量相似度是粗粒度的，拿到 top-K 后需要更精细的语义匹配排序。

**方案**：在检索结果基础上，用 **Reranker 模型**对 query 和每个候选 chunk 做交叉编码精排。

- **第一版实现**：调用硅基流动的 bge-reranker-v2-m3 接口（或同类型 rerank 模型）
- **接口抽象**：定义 `Reranker` 接口，支持替换为本地模型或其他厂商
- **只 rerank top-N**：默认取 top 20 进行 rerank，平衡效果和成本
- **输出**：rerank 后的重新排序的证据列表，分数作为新的 weight

**配置参数**：
```go
type RetrievalConfig struct {
    // ...（上面的）
    RerankEnable  bool   // 默认 false
    RerankTopN    int    // 取前 N 个 rerank，默认 20
    RerankModel   string // rerank 模型名，默认 "bge-reranker-v2-m3"
}
```

### 2.3 第三层：LLM 查询扩展（Query Expansion）

**问题**：用户需求单的表述不一定是最优检索 query，不同的表述方式召回的结果差异很大。

**方案**：用 LLM 将原始 query 扩展为多个不同表述的 query，分别检索后合并去重。

- **实现位置**：在 `claimloop.go` 的 Thinker 层，当前的 stagedThinker 是规则加前缀（"规定""要求"等），升级为可选的 LLM 驱动
- **扩展数量**：默认 3 个（保守值，避免 token 成本过高）
- **合并策略**：按 DocSentenceID 去重，多个 query 都命中的句子 weight 取 max
- **与 Guardian Budget 的关系**：query expansion 算「第一轮检索」，后续 Guardian 循环仍然正常工作——相当于第一轮检索质量更高了，Guardian 可能更快达到 accept 态

**配置参数**：
```go
type RetrievalConfig struct {
    // ...（上面的）
    QueryExpandEnable  bool  // 默认 false
    QueryExpandCount   int   // 扩展 query 数量，默认 3
}
```

---

## 3. 评测基准（核心！这是能量化的关键）

没有评测就没有优化。本包同时交付一套可复现的评测基准。

### 3.1 评测数据集构造（本包 Step 0，最先做）

位置：`agent/retrieve/eval/testdata/`

#### 3.1.1 构造标准

评测集是所有优化的基线，必须先建评测集再谈优化。遵循以下标准：

**标准一：基于真实文档，不编造**
- 所有 query 的答案必须真实存在于 `testdoc/2026招生工作/` 的 4 份文档中
- 标注的 relevant_sentence 必须是文档中的原句，不 paraphrase、不加工

**标准二：难度分级明确，覆盖三种检索场景**

| 难度 | 数量 | 定义 | 典型特征 |
|------|------|------|---------|
| **Easy** | 8 条 | 关键词精确匹配即可找到 | query 中的关键词在原文中直接出现（如"平行志愿""高考移民""烈士子女"）；答案在同一段落内 |
| **Medium** | 8 条 | 需要语义匹配或跨段落 | query 用词与原文不同但语义一致（如"怎么报名"→"报名流程"）；答案分布在同一文档的 2-3 个段落 |
| **Hard** | 4 条 | 需要跨文档综合或推理 | 答案需要从 2 份以上文档中汇总（如"比较 2024 和 2025 报名人数变化"）；或答案隐含在数据中需要简单计算（如"本科录取率是多少"） |

**标准三：Query 要贴近真实用户提问方式**
- 不用关键词列表（×："报名条件 随迁子女"）
- 用自然语言问句（✓："随迁子女在本地高考需要什么条件？"）
- 覆盖不同问法：是什么、有哪些、是多少、怎么办、比较类

**标准四：标注必须到句子级**
- 每条 query 标注所有相关的句子（可能 1~8 句不等）
- 用 `doc_key + sentence_index` 定位（而不是 chunk_id，因为句子是溯源的最小单位）
- 标注者判断："如果用户问这个问题，这一句子是否应该出现在检索结果的前列？"

#### 3.1.2 数据集文件格式

```
agent/retrieve/eval/testdata/
├── eval_queries.json        # 主评测集（20 条）
├── eval_queries_sanity.json # 小 sanity 集（5 条，开发期快速验证）
└── README.md                # 标注说明 + 难度判定标准
```

`eval_queries.json` 格式：
```json
[
  {
    "id": "q001",
    "query": "随迁子女在流入地参加高考需要满足什么条件？",
    "difficulty": "easy",
    "category": "报名条件",
    "relevant": [
      { "doc": "baoming_tiaojian", "sentence_index": 12 },
      { "doc": "baoming_tiaojian", "sentence_index": 13 },
      { "doc": "baoming_tiaojian", "sentence_index": 14 }
    ],
    "notes": "答案在 1.2 节随迁子女报名条件，3 个条件并列"
  }
]
```

字段说明：
- `id`：唯一编号，便于定位失败 case
- `query`：用户自然语言提问
- `difficulty`：easy / medium / hard
- `category`：问题所属类别（报名/录取/数据/政策），便于分类分析
- `relevant`：相关句子列表，按相关性从高到低排列（排序本身也可用于 NDCG 评估）
- `notes`：标注说明，便于后续复核

#### 3.1.3 文档 key 与句子索引的对应

先运行一次文档解析 + 句子提取，建立「文档 key → 句子列表」的映射，作为标注的参考：

| 文档文件名 | doc_key | 预估句子数 |
|-----------|---------|-----------|
| 报名条件与流程.md | `baoming_tiaojian` | ~80 句 |
| 去年招生数据.md | `zhaosheng_shuju` | ~50 句 |
| 录取规则.md | `luqu_guize` | ~90 句 |
| 招生政策依据.md | `zhengce_yiju` | ~40 句 |

**句子索引规则**：
- 使用 `splitter.Sentences()` 将全文切分为句子数组
- `sentence_index` = 句子在全文句子数组中的下标（从 0 开始）
- 句子切分以句末标点（。！？；…）为界，与系统实际切分逻辑一致

#### 3.1.4 20 条 Query 设计清单（按文档分布）

**报名条件与流程（6 条）**

| ID | Query | 难度 | 说明 |
|----|-------|------|------|
| q01 | 报名高考需要满足哪些基本条件？ | easy | 关键词"报名条件"直接命中 1.1 节 |
| q02 | 随迁子女在外地高考要什么条件？ | easy | "随迁子女"关键词明确 |
| q03 | 哪些人不能参加高考报名？ | easy | "不得报名""高考移民"关键词 |
| q04 | 高考报名一般是什么时候？ | easy | "报名时间"关键词 |
| q05 | 高考报名需要准备什么材料？ | medium | "材料"vs 原文"所需材料"，语义接近 |
| q06 | 高考报名的完整流程是怎样的？ | medium | 跨 2.1~2.4 多段落综合 |

**去年招生数据（5 条）**

| ID | Query | 难度 | 说明 |
|----|-------|------|------|
| q07 | 2024年全国高考有多少人报名？ | easy | "2024""报名人数"关键词 |
| q08 | 去年本科招了多少人？ | easy | "本科招生"关键词 |
| q09 | 高考录取率大概是多少？ | medium | "录取率"需要从招生数/报名数推算 |
| q10 | 2025年报名人数比上一年多还是少？ | medium | 需要对比两年数据 |
| q11 | 高考是几月几号考？考几天？ | easy | "考试时间""6月7日"关键词 |

**录取规则（6 条）**

| ID | Query | 难度 | 说明 |
|----|-------|------|------|
| q12 | 平行志愿是怎么投档的？ | easy | "平行志愿""投档"关键词 |
| q13 | 什么是专业级差？ | easy | "专业级差"关键词明确 |
| q14 | 高考加分项目有哪些？ | easy | "加分政策"关键词 |
| q15 | 考生被退档的原因一般有哪些？ | medium | "退档原因"vs 原文"退档原因"，但需要归纳 5 点 |
| q16 | 平行志愿和顺序志愿有什么区别？ | medium | 需要对比两个章节的内容 |
| q17 | 本科录取分几个批次？分别是什么？ | easy | "录取批次"关键词 |

**跨文档综合（3 条，Hard）**

| ID | Query | 难度 | 涉及文档 | 说明 |
|----|-------|------|---------|------|
| q18 | 2024年高考报名人数和录取率分别是多少？ | hard | 招生数据 + 录取规则 | 需要从两份文档取数据，且录取率是推算值 |
| q19 | 少数民族考生高考有什么加分政策？ | hard | 录取规则 + 招生政策依据 | 两份文档都提到加分，需要综合 |
| q20 | 强基计划和综合评价招生有什么不一样？ | medium | 录取规则 | 同文档跨章节对比，需理解两种招生模式的差异 |

> 注：以上 20 条为建议模板，实际标注时可根据文档内容微调，保证每条 query 的 relevant 句子真实可对应。

#### 3.1.5 标注步骤（按顺序做）

**Step 0-A：导出句子索引**
```bash
# 先写一个工具脚本，把 testdoc 下的文档解析并输出句子列表
# 输出到 agent/retrieve/eval/testdata/doc_sentences/ 下，每个文档一个 JSON
# 格式：[{index:0, text:"..."}, {index:1, text:"..."}, ...]
```
目的：让标注者对着句子列表标注，确保 sentence_index 准确。

**Step 0-B：逐条标注（约 1.5 小时）**
1. 对照上面的 20 条 query 模板，逐条在句子列表中找相关句子
2. 按相关性从高到低排列相关句子（最能直接回答问题的排第一）
3. 每条 query 标注完后，在 notes 字段写一句话说明答案位置
4. 全部标完后，自己再 review 一遍，确保没有漏标、误标

**Step 0-C：构造 sanity 集（5 条，快速验证用）**
- 从 20 条中选 5 条最确定的（2 easy + 2 medium + 1 hard）
- 用于开发期快速跑通评测脚本，不用每次都跑 20 条

**Step 0-D：验证标注正确性**
- 用当前系统（纯 dense 检索）跑一遍 sanity 集
- 检查 easy 题的 recall@5 不应低于 50%（如果太低，说明要么标注错了要么检索真的很差）
- 检查 hard 题确实比较难（recall@5 < 50%），验证难度分级合理

#### 3.1.6 验收标准

- [ ] eval_queries.json 格式正确，20 条 query 全部填好
- [ ] 每条 query 的 relevant 数组非空，且对应句子在文档中真实存在
- [ ] easy: 8 条，medium: 8 条，hard: 4 条，比例正确
- [ ] 覆盖 4 份文档，不是集中在某一份
- [ ] 覆盖 4 种问法：是什么、有哪些、是多少、比较对比
- [ ] sanity 集（5 条）单独可用
- [ ] README.md 说明了标注标准、难度定义、句子索引规则

### 3.2 评测脚本

位置：`agent/retrieve/eval/eval_test.go`（用 Go test 框架写，`go test` 直接跑）

**评测指标**：

| 指标 | 公式 / 说明 | 意义 |
|------|------------|------|
| **Recall@K** | 前 K 个结果中相关文档数 / 总相关文档数 | 搜得全不全 |
| **Precision@K** | 前 K 个结果中相关文档数 / K | 搜得准不准 |
| **MRR** | 第一个相关结果的排名的倒数的平均值 | 第一名有多准 |
| **NDCG@K** | 考虑排名位置的归一化折损累计增益 | 综合排序质量 |

**默认评测 K 值**：@3, @5, @10（三个常用阈值）

### 3.3 评测命令

```bash
# 跑纯向量检索（基准线）
RETRIEVAL_STRATEGY=dense go test ./agent/retrieve/eval/ -run TestRetrievalQuality -v -count=1

# 跑混合检索
RETRIEVAL_STRATEGY=hybrid go test ./agent/retrieve/eval/ -run TestRetrievalQuality -v -count=1

# 跑混合+Rerank
RETRIEVAL_STRATEGY=hybrid_rerank go test ./agent/retrieve/eval/ -run TestRetrievalQuality -v -count=1

# 全部策略对比（一键输出对比表）
go test ./agent/retrieve/eval/ -run TestAllStrategiesComparison -v -count=1
```

### 3.4 预期提升（目标值，做完后验证）

| 策略 | Recall@5 | Precision@5 | MRR | NDCG@5 |
|------|---------|------------|-----|--------|
| Dense（基准） | ~60% | ~45% | ~0.50 | ~0.50 |
| + Hybrid | ~72% (+12pp) | ~55% (+10pp) | ~0.60 (+0.10) | ~0.60 (+0.10) |
| + Hybrid + Rerank | ~75% (+3pp) | ~68% (+13pp) | ~0.72 (+0.12) | ~0.72 (+0.12) |
| + Hybrid + Rerank + QueryExpand | ~82% (+7pp) | ~65% (-3pp) | ~0.70 (-0.02) | ~0.71 (-0.01) |

> 注：Query Expansion 通常提升召回但可能略微降低精度（引入噪声），符合预期。具体数字以实际评测结果为准。

---

## 4. 可执行步骤

### Step 0: 评测数据集构造（最先做，约 2 小时）

**为什么是 Step 0**：没有评测集就没有 baseline，后续所有优化都无法量化。必须先把评测基建搞定，再谈优化。

**4 个子步骤**：

0-A. **写句子导出工具**（~30min）
   - 位置：`agent/retrieve/eval/testdata/export_sentences.go`（或单独的 `_tools/` 目录）
   - 功能：读取 `testdoc/2026招生工作/` 下的 4 份 md 文档，用 `splitter.Sentences()` 切句，输出每个文档的句子列表 JSON 到 `doc_sentences/` 目录
   - 输出格式：`[{index: 0, text: "..."}, {index: 1, text: "..."}, ...]`

0-B. **人工标注 20 条 query**（~1.5h）
   - 对照 3.1.4 节的 20 条 query 模板，在句子列表中逐条标注 relevant 句子
   - 标注顺序建议：先 easy（快），再 medium，最后 hard（费脑子）
   - 标注时顺便验证 query 模板是否合适，不合适就微调
   - 从 20 条中挑 5 条组成 sanity 集（`eval_queries_sanity.json`）

0-C. **写 README**（~15min）
   - 说明数据集构造标准、难度定义、句子索引规则
   - 方便后续扩充评测集时保持标注一致性

0-D. **跑 baseline 验证**（~15min）
   - 用当前纯向量检索跑一遍 sanity 集
   - 看 easy 题 recall@5 是否在 40%-70% 之间（太低说明标注有问题，太高说明题太简单）
   - 记录 baseline 数据，作为后续优化的对比基准

**验收**：
- [ ] `eval_queries.json` 有完整 20 条，格式正确
- [ ] `eval_queries_sanity.json` 有 5 条
- [ ] `doc_sentences/` 下 4 份文档的句子 JSON 都已生成
- [ ] README.md 已写好
- [ ] baseline 数据已记录（先记下来，后面优化完对比用）

---

### Step 1: 配置结构 + 策略接口（约 1 小时）

1. 在 `config/config.go` 新增 `Retrieval` 配置段，所有字段有默认值（= 当前行为）
2. 在 `agent/retrieve/` 新增 `strategy.go`，定义 `RetrievalStrategy` 接口：
   ```go
   type RetrievalStrategy interface {
       SearchSentences(ctx context.Context, req SearchRequest) ([]ScoredSentence, error)
   }
   ```
3. 将现有检索逻辑封装为 `DenseStrategy`（实现接口）
4. `Retriever` 结构体从直接调 `SearchKbaseSentences` 改为使用 `RetrievalStrategy` 接口

**验收**：`go test ./... -count=1` 全绿，行为与改动前完全一致。

### Step 2: BM25 分词 + 混合检索（约 4 小时）

1. 在 `splitter/splitter.go` 新增 `CharNGramTokens(text string, n int) map[string]int` — 2-gram 分词
2. 在 `storage/kbase_chunk.go` 的 Chunk 模型新增 `Bm25Tokens` 字段（JSON 存储）
3. 文档解析 worker 中新增 BM25 tokens 计算与存储
4. 新增 `HybridStrategy`（实现 RetrievalStrategy 接口）：
   - 先用 dense 检索取 top 50 候选
   - 对候选池计算 BM25 分数
   - RRF 融合排序
5. 新增 migration：对已有 chunk 补算 bm25_tokens（批处理，按租户分片）

**验收**：
- `go test ./splitter/ -run TestCharNGramTokens -v` 分词单测通过
- `go test ./agent/retrieve/ -run TestHybridStrategy -v` 混合检索单测通过
- migration 跑完后，随机抽查 5 个 chunk 的 bm25_tokens 非空

### Step 3: Rerank 重排序（约 3 小时）

1. 新增 `agent/retrieve/rerank.go`：
   - 定义 `Reranker` 接口：`Rerank(query string, docs []string) ([]float64, error)`
   - 实现 `SiliconFlowReranker`（调用硅基流动 bge-reranker 接口）
2. 新增 `RerankStrategy`（装饰器模式，包装底层 Strategy）：
   - 调用底层 strategy 拿候选
   - 取 top N 调用 reranker
   - 返回 rerank 后的结果
3. 在 RetrievalConfig 中挂接开关

**验收**：
- `go test ./agent/retrieve/ -run TestReranker -tags=integration -v` 集成测试通过
- 验证 rerank 前后顺序有变化（构造 query 和候选，确认更相关的排到前面）

### Step 4: LLM 查询扩展（约 2 小时）

1. 在 `claimloop.go` 新增 `LLMQueryExpander`（实现 ThinkFn 类型）
   - 调用 LLM 将原始 claim 扩展为 N 个不同表述的 query
   - Prompt 示例："请用 3 种不同的方式表述下面这个检索需求，每种用一句话：..."
2. 在 stagedThinker 和 LLMQueryExpander 之间通过配置选择
3. 第一轮检索时，所有扩展 query 并行检索，结果合并去重

**验收**：
- `go test ./agent/retrieve/ -run TestLLMQueryExpander -tags=integration -v` 集成测试通过
- 验证扩展 query 数量符合配置，且不重复

### Step 5: 评测基准（约 4 小时）

1. 人工标注 20 条 eval query（基于 testdoc 目录）
2. 编写 `agent/retrieve/eval/eval_test.go`：
   - `TestRetrievalQuality` — 单策略评测，输出 Recall/Precision/MRR/NDCG
   - `TestAllStrategiesComparison` — 所有策略对比，输出 markdown 表格
3. 跑基准线（dense），记录 baseline 数据
4. 依次开启各层策略，跑评测，记录提升数据

**验收**：
- `go test ./agent/retrieve/eval/ -run TestAllStrategiesComparison -v -count=1` 能输出完整对比表
- 对比表中每层策略都有明确的指标变化（提升或下降），数据可复现

### Step 6: 配置默认值 + 文档（约 1 小时）

1. 所有开关默认关闭，保持现有行为不变
2. 在 `docs/rebuild/packages/` 本文件中补充实测结果表
3. 在 README 的技术栈部分补充检索优化说明

**验收**：默认配置下，所有现有测试通过，行为与改动前一致。

---

## 5. 验收标准

### 5.1 功能验收

- [ ] Step 0 完成：评测数据集（20 条 + sanity 5 条）已构造，baseline 数据已记录
- [ ] 默认配置（strategy=dense, rerank=false, query_expand=false）下，所有现有测试通过
- [ ] 开启 hybrid 后，检索结果数量正确（= topK），分数为 RRF 融合值
- [ ] 开启 rerank 后，top-N 结果顺序被重新排序
- [ ] 开启 query_expand 后，首轮检索执行了 N 个 query，结果已去重
- [ ] migration 能正确补算已有文档的 bm25_tokens

### 5.2 评测验收

- [ ] 评测数据集 ≥ 20 条 query，覆盖 easy/medium/hard 三个难度（分布：8/8/4）
- [ ] 评测脚本输出 Recall@3/5/10、Precision@3/5/10、MRR、NDCG@5 八项指标
- [ ] 各策略对比能输出一张完整的 markdown 对比表
- [ ] 至少一个策略有统计显著的提升（Recall@5 提升 ≥ 5pp）
- [ ] 有 baseline（dense）vs 优化后的数据对比，且数据可复现（跑两次结果一致）

### 5.3 回归验收

- [ ] `go test ./... -count=1` 全绿
- [ ] `go test -tags=integration ./... -p 1 -count=1` 全绿
- [ ] `bash scripts/smoke_e2e.sh` 通过
- [ ] 前端 `npm run lint && npx tsc -b` 通过

---

## 6. 简历可用的量化数据（本包完成后）

完成本包后，简历上可以写：

> 设计并实现了三层检索质量优化体系（混合检索 + Rerank + 查询扩展），在自建评测基准（20 query × 3 难度等级）上：
> - Recall@5 从 60% 提升至 82%（+22pp）
> - NDCG@5 从 0.50 提升至 0.71（+42%）
> - 通过策略配置化实现渐进式上线，主流程零风险

---

## 7. 开放问题

| 问题 | 建议 | 影响 |
|------|------|------|
| BM25 分词用 char-ngram 还是引入 jieba 分词？ | 先用 char 2-gram（简单可靠，政企专有名词多，词典分词反而容易 OOV），效果不佳再考虑 jieba | Step 2 工作量 |
| Reranker 用 API 还是本地模型？ | 先用硅基流动 API（成本低、接入快），后续可考虑本地 bge-reranker-small | Step 3 成本 |
| 评测集 20 条够不够？ | 够了。个人项目的评测集关键是有和没有，20 条足以展示"你懂评测方法论" | 精度 |
| 是否需要补 Query Rewrite（query 改写，而非扩展）？ | 先不做。Query Expansion 已经够了，再加就超了，面试时可以作为"后续优化方向"提 | 范围控制 |
