# content-hub · 稿件协作多 Agent 化重建 — 总索引 / 依赖矩阵 / 验收

> 这是把 `docs/rebuild/proposals/rfc-cooperative-authoring-multi-agent-rebuild.md`(完整 RFC/理由书,全文保留)落成**可开工、可验收**的工作包索引。
> RFC 回答"为什么/改成什么样"；本目录回答"按什么顺序改、改哪个文件的哪里、怎么才算验过、还差哪个决策需要人拍板"。
> 读完本 README 即可开工；每个 `packages/NN.*.md` 是一份**单点可提交、有独立验收**的执行单，可交付码农（或你自己）照做。

---

## 1. 为什么拆包、以及"包"如何保证不被架空

- 拆分目的：RFC 仍负责"论证充分/可对线"，`packages/` 负责"能照着写完且每步能验"，避免"文档很丰满、开工却卡在细节/顺序/验收不明确"。
- 每份 work package 含三段硬货，缺一不可：
  1. **范围与命中代码**（涉及文件 + 函数/表的精确位置）——没有它就去翻 RFC，避免改错层。
  2. **可执行步骤**（先做什么、怎么做、兼容旧数据怎么办）——不是"我们要 X"，是"改掉这几个点能达到 X"。
  3. **验收标准**（能跑的命令/断言/回归）——"做到什么才算这个包 done"，不是软指标。
- **顺序即编号**：P01–P02 是 rev-3 的 C 级安全/并发地基；P03–P04 把"结构化+sources"两个命根补齐；P05–P07 把多 agent 引擎从"函数链"落到 r/step/Guardian 三个真循环；P08–P10 开放"人可改稿/拿来稿"（rev-3/4 手编与 draft-assist）；P11–P12 是"用户看得到可溯源+知道在干嘛"的前端体验（rev-2/4 UX）。必须先完成上排**其依赖列**的那些包，再开本包。

---

## 2. 顺序与依赖矩阵（改前请过一遍）

| # | 工作包 | 原则主张(出处) | 依赖包 | 主要涉及 repo 点位 |
|---|--------|----------------|--------|--------------------|
| P01 | 检索可见性平面(越权旁路) | rev-3 §12.3 / C1 | — | `storage/qdrant.go`,`api/service/kbase_search.go`,`api/service/qa.go` |
| P02 | 稿件版本乐观锁(CAS) | rev-3 §12.4 / C2 | — | `storage/article.go`,`api/service/{revise,append,generation}.go` |
| P03 | 结构化层级真落库+response带结构 | rev-4 §13.1 / W1 | — | `api/service/generation.go`,`api/service/revise.go`,模型 `article_sentences` |
| P04 | 证据绑定的"人读 source + has_newer" | rev-2 §8.2-Q2 / §10.1, rev-4 §13.6 / W6 | P03 | `storage/article.go`,`api/handler/article.go`(response) |
| P05 | agent run 状态机与持久体(run/step) | rev-1 §2.1, rev-3 ·D1 | P01,P02 | 新 `agent/coordinator/` + `agent_run/agent_step` 表 |
| P06 | 检索 Agent 真多轮 + Guardian 三态裁决 | rev-1 §3.2/§3.3, Q1 | P05 | `agent/retrieve`,`agent/orchestrator`,新 Guardian |
| P07 | Verify 先规则 + 删冗余首检 + 弃置双实现 | rev-1 §3.1; rev-3 C5/C6 | P05 | `agent/censor`,`agent/orchestrator`,`api/service/revise.go` |
| P08 | 稳定句身份 + 序列表单(增/删/移/前插) | rev-4 §13.3 / W3 | P02,P03 | `api/service/sequence.go`(新),`article_sentence` 用法 |
| P09 | 人机混合写作受控编辑 + no_source 黄点 | rev-3 §12.5 | P08,P04 | `web` WorkspaceDetail,`run:revision`复用 |
| P10 | draft_assist(拿样稿起稿)并入轨 | rev-4 §13.5 / W5 | P09,P04 | requirement/REST,splitter 复用 |
| P11 | 稿件结构化渲染 + 证据 tooltip + 版链提示 | rev-2 §9.1/§9.2, rev-4 W6 | P03,P04,P09 | `web` 稿件 view/详情 |
| P12 | workspace 状态机 UX + 对话"人话"结果 + 可生成硬前置 | rev-4 §13.2/§13.4 / W2/W4 | P06 | `web`,workspace/requirements 状态 |

> **注**
> - P03 会同时修正旧历史快照问题（一次批处理降级 vs 重建）——详见包内"兼容/旧数据"。
> - P08 必须排在 P09 之前：P09 开放手编/就地把改，没有稳定 `sentence_id`+ 增删改换，一放开就乱（见 RFC rev-4 §13.3）。
> - P05 的 run 持久体会换来 HTTP 端"异步 + 可回放 + 可审"——它也是 rev-1 要撑住 A2(持久副作用)的那个点；这包会连带出 `article_generate` 队列被 worker 真消费的改法(rev-1 §3.5)。

---

## 3. 包与「面试若问」的映射（评审口径，不散在包里看）
- "你这是不是 workflow?" → **rev-1 §6 (A1/A2/A3 口径)**；实现证据在 **P06(Guardian 三态 + Persistent run/step)**。
- "可信怎么保证?" → **rev-1 §3.1 RuleVerifier(规则优先)**；落在 **P07**。
- "谁来可溯源/让我看懂一句出自哪?" → **P04 + P11**（source + tooltip）；RFC rev-2 §9.2/§10.1。
- "AI 稿能不能让我改?" → **P08+P09**；RFC rev-3 §12.5 / rev-4 W3。
- "我怎么知道在干嘛/下一步干嘛?" → **P11 + P12**；RFC rev-2 §9.4, rev-4 W2/W4。
- 安全(越权) → **P01**；并发一致性 → **P02**。拿到简历里是"实现层也防了"的铁证。

---

## 4. 直接可用命令
```bash
# 后端纯单测（不依赖外部服务）
go test ./... -count=1
# 集成(需配置齐)：先起 mysql/redis/qdrant/rabbitmq，再
go test -tags=integration ./... -count=1 -p 1
# E2E 冒烟 + 隔离对抗（需 api+worker 已在跑）
bash scripts/smoke_e2e.sh
cd web && npm run lint && npx tsc -b
```
每个包内会给更聚焦的验收，但上面这四条是你手工验收的兜底基线——任何包不得让既有基线变红。

---

## 5. 待用户/评审拍板项(跨包,标在最前)
| 需要拍板 | 影响哪个包 | 默认建议 |
|----------|-----------|---------|
| run 表是否一步到位建 `agent_runs/agent_steps`(而非先用 conversation 消息扩) | P05 | 一步建表,P05 内已给字段草案 |
| 历史快照:旧已生成稿是"降级为线性文本"还是尝试重建结构 | P03 | 默认"降级为线性+标旧",避免从仅 full_content 反推产生错位 |
| 是否一期就开放 hand-edit(富文本 vs 句段级) | P09/P11 | 先做句/段级受控;全稿富文本后推 |
| 公库"AI 可引用"边界(允许普通用户对公库做问答/生成引用,但不可下载) | P01 | 允许引用(这与 features 公库可引用一致),仅封禁下载/写 |

> 详见各 work package 内“开放问题”。阶段 gate：每个包合并前至少其"依赖包"已完成且基线绿。
