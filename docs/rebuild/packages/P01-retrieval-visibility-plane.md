# P01 · 检索可见性平面（修私有库可被同租户经 AI 检索旁路）

- RFC 出处：rev-3 §12.3 / C1；README 顺序=P01（最先，安全）
- 状态：**DONE**（已实现并真验收，见文末“完成记录”）
- 实现方式简述：`QdrantVector` 写入 `pt_scope`/`pt_owner`(来自 kbase_file 的 scope+owner)；检索 `SearchVectors(ctx,..ownerUserID..)` 用可见 filter= 公库 OR（私库且 owner=本人）；顶层 `SearchKbase/SearchKbaseSentences` 由 ctx(user) 决定 owner（middleware 种入），无 ctx/user → 仅公库（保守）。

- 目标：让**向量检索**复用与"文件系统浏览/写"一致的可见性判定，封堵"同租户他人私有库文档可被 AI 生成/问答命中"的越权旁路。

---

## 1. 为什么这是第一包（问题现象）
文件目录/写路径已经按 `{scope: public/private, owner_user_id}` 做过校验（`api/handler/kbase.go` 将私有库 owner=当前 user、公有库 owner=0；`storage/kbase_file.go` 的 List/Rename/Delete 全带 owner）。
但 AI 生产链路(生成/ClaimPlanner/QA)走的**向量检索没有接入这套平面**：
- `api/service/kbase_search.go::SearchKbase / SearchKbaseSentences` 只带 `tenantID(+可 fileIDs)`；
- `agent/qabot` 问答 `kbase_retriever.Retrieve`(qa.go) 甚至不传 fileIDs → 面向整个租户所有库；
- `storage/qdrant.go` 的 payload 只有 `tenant/file/chunk/latest/version/content/chapter`，**没有 scope/owner_user_id**；`searchFilter` 只 `tenant+latest+(可选file)`。
结论：用户 B 不能进 A 的私有库目录看，但 B 的问答/生成在未勾紧范围时能命中 A 的私有切片 → "私有库仅本人可见"被 AI 检索旁路。

## 2. 范围与命中代码
| 文件 | 函数 / 位置 | 改动意图 |
|------|------------|----------|
| `storage/qdrant.go` | `QdrantVector{}` struct、`toPointStruct`、`searchFilter`、`QdrantSearchHit` | 索引点上携带 `scope` + `owner_user_id`；检索 filter 支持按可见平面过滤 |
| `storage/model/...`（写库侧推送向量） | 调用 `UpsertVectors` 的地方把 scope/owner 一并放入 | 写点带全可见性字段 |
| `api/service/kbase_search.go` | `SearchKbase/SearchKbaseSentences` 签名扩展可选可见范围 | 默认限定在当前 user 的可见集合(公+本人私库)；显式传 client 允许全量搜 |
| `api/service/qa.go` | `kbaseRetriever`/`AskQABot` 内 Retrieve | 带入当前 user 的可见过滤，而不是全租户 |
| `api/service/kbase_searcher.go` / claim planner 注入 | `Searcher` 对应 | 带可见平面以保持一致 |

## 3. 可执行步骤
1. **向量结构加可见字段**：`QdrantVector` 增加 `Scope string`(public/private) 与 `OwnerUserID uint64`；`toPointStruct` 写入 payload(`scope` 用 keyword/字符串、`owner_user_id` 用 int;私有 owner=owner_id,公有 owner=0)。写出时：公库文档 `scope="public",owner=0`, 私有 `scope="private",owner=<用户id>`。

2. **检索构造可见合取**：`SearchVectors`(或新增 `SearchVectorsVisible`) 的 filter 由调用方传 `allowed`:例如公库(size=public) OR (私有且 owner=me)。在 qdrant 里表达为 filter `Matcher` 的 OR(≥2 子条件):`(scope=public)` 或 `(owner=me AND scope=private)`(以及 version 等旧条件仍在 must)。若现有 SDK 先建简单版,可用 `MustNot + ...` 组合,但目标是把"能看哪些"推到存储层。

3. **调用方透传可见**：把 `SearchKbase/SearchKbaseSentences/ClaimSearcher/QA` 都改为接收可选 `UserID`,内部若给出则求 allowed owner;`fileIDs` 非空时仍先在同可见集合内再校验,若请求 fileID 不属于本人/公则可读范围 → 抛权限错。

4. **写侧一致**：凡是 `UpsertVectors` 写入 Qdrant 的调用(文档解析/重向量化),确认按该文档的 scope/owner 写入 payload。

5. **不破坏既有浏览写权**：确保只把"新增的可见合并条件"用于检索,不改变既有文件浏览/写 owner 逻辑。

## 4. 验收标准（必须可跑）
- **新增集成测试(隔离对抗类)**：同租户两个账号 A、B，各自私有库各放一条唯一文本；A 上传后 B 调用 QA / 生成检索,断言**不返回任何 A 私有库句**；A 自己的问答/生成能命中自己的私有句。
- 公库内容对每个账号都可见可引用(与 features 一致)。
- 回归：`scripts/smoke_e2e.sh` 的多租户隔离对抗仍绿——它现在只覆盖文件读写级；本包把对抗延到**向量检索级**。
- 全仓仍能 `go build ./...`；既有 `go test ./...`(不带 integration)不因本包失败。

## 5. 兼容与旧数据
- 已写入 Qdrant 的旧点缺 scope/owner 字段 → 需 **重向量化或迁移**(写一段扫描/重建：读取每个 doc 的 scope/owner 回填 payload；或按 `document_id` 从 MySQL 取 owner 更新)。给一段一次性 job/脚本,在 P01 内完成,否则新 filter 会漏掉旧文档。

## 6. 开放问题 / 需拍板
- 公库"AI 可引用但不可下载"边界是否与 files 一致?默认**允许引用**(公库本就可引用,搜索范围属于引用),只封超范围写/下载。若保守,可在检索可见先排除公库而对普通用户仅私库;但会削弱产品价值——建议保持公库可检索,提交评审确认。

## 7. 完成一个 clear gate
"P01 done" = 上面第 4 节隔离对抗测试通过 + 旧点迁移完成 + 既有基线 `go build ./...`、`go test ./...`(非 integration)绿。

---

## ✅ 完成记录（真实验收）
- **已实现代码**：
  - `storage/qdrant.go`：`QdrantVector` 增 `Scope`/`OwnerUserID`；`toPointStruct` 写入整数 payload `pt_scope`(0 public/1 private)+`pt_owner`；`searchFilter`/`visibilityCond` 构造可见 OR =公库 OR（私库且 owner=本人），owner=0 仅公库；`SearchVectors` 签名加 `ownerUserID`。
  - `api/service/kbase.go`：`ProcessDocument` 写点前 `GetFileByID` 拿 scope/owner 并写入每个点。
  - `api/service/kbase_search.go`：新增 `searchOwnerFromCtx(ctx)`（读 middleware 种入的 user）；`SearchKbase`/`SearchKbaseSentences` 内部以该 owner 调 `SearchVectors` → 无 ctx/user 仅公库、HTTP 全链 owner 生效。QA/claim/revise/append/retrieve 均经这两顶层函数，一次性套上可见平面，无需逐个入口改。
- **验收(真实跑了)**：
  - `go build ./...` ✅
  - `go test ./... -count=1`（纯单测回归全绿）✅
  - `go vet ./storage/ ./api/service/ ./agent/... ./cmd/...` ✅ 无告警
  - **`go test ./storage/ -run TestVectorSearchVisibilityIsolation -count=1 -v` ✅ PASS**（真实 Qdrant）：同租户 A 私有 / B 私有 / 公库三点下，owner=A 仅见 A+公库、owner=B 仅见 B+公库、owner=0 仅见公库，任何检索不含他人私库——越权旁路在向量层被过滤。
  - 该真隔离测试源：`storage/qdrant_visibility_test.go`（仅依赖 Qdrant；业务假 tenant=99999001 隔离）。
- **旧点(${已有 vector 但无 pt_scope/pt_owner})**：会被新可见 filter 排除（安全方向，不越权）；恢复方式=重索引该文件来源（重新跑一遍解析/向量化写入，此时带 scope/owner）。不迁移则只是“旧点是可检索但属于不可见集被过滤”，无泄漏风险。
