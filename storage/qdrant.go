package storage

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"

	"github.com/WWaynee/content-hub/config"
)

// Qdrant 向量库封装：写入/检索按租户隔离 + latest 版本过滤。

var QdrantClient *qdrant.Client

// DefaultVectorSize 向量维度，与 Embedding 模型一致（Qwen3-VL-Embedding-8B = 4096）。
// 后续连接真实 embedding 时，若维度不同需同步此值。
const DefaultVectorSize = uint64(4096)

// InitQdrant 初始化 Qdrant 客户端并确保集合存在。
func InitQdrant(vectorSize uint64) error {
	cfg := config.Get().Qdrant
	client, err := qdrant.NewClient(&qdrant.Config{
		Host:                    cfg.Host,
		Port:                    cfg.GRPCPort,
		SkipCompatibilityCheck:  true,
	})
	if err != nil {
		return fmt.Errorf("初始化 Qdrant 客户端失败: %w", err)
	}
	QdrantClient = client

	return ensureCollection(context.Background(), collectionName(), vectorSize)
}

func collectionName() string { return "content_hub_kbase" }

func ensureCollection(ctx context.Context, name string, vectorSize uint64) error {
	exists, err := QdrantClient.CollectionExists(ctx, name)
	if err != nil {
		return fmt.Errorf("检查集合是否存在失败: %w", err)
	}
	if exists {
		return nil
	}
	err = QdrantClient.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: name,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     vectorSize,
			Distance: qdrant.Distance_Cosine,
		}),
	})
	if err != nil {
		return fmt.Errorf("创建向量集合失败: %w", err)
	}
	return nil
}

// QdrantVector 要写入的一条向量 + 元数据。
type QdrantVector struct {
	ID           uint64    // 点全局唯一 ID
	TenantID     uint64    // 租户（隔离键）
	FileID       uint64    // 文档 ID
	Scope        string    // scope=public/private（可见性平面）
	OwnerUserID  uint64    // owner_user_id：private 库为归属用户，public 库为 0
	VersionMd5   string    // 版本
	ChunkIndex   int       // 切片序号
	Content      string    // 切片原文（检索直接返回）
	ChapterTitle string    // 章节标题
	Latest       bool      // 是否最新版本（检索过滤键）
	Vector       []float32
}

func toPointStruct(v QdrantVector) (*qdrant.PointStruct, error) {
	payload := map[string]*qdrant.Value{}
	intVals := map[string]int64{
		"tenant_id":   int64(v.TenantID),
		"file_id":     int64(v.FileID),
		"chunk_index": int64(v.ChunkIndex),
		"pt_scope":    scopeCode(v.Scope),  // 0=public, 1=private，可见性过滤
		"pt_owner":    int64(v.OwnerUserID), // private 归属用户；public 为 0
	}
	for k, val := range intVals {
		pv, err := qdrant.NewValue(val)
		if err != nil {
			return nil, err
		}
		payload[k] = pv
	}
	latest := v.Latest
	lv, err := qdrant.NewValue(latest)
	if err != nil {
		return nil, err
	}
	payload["latest"] = lv

	for k, s := range map[string]string{
		"version_md5":   v.VersionMd5,
		"content":       v.Content,
		"chapter_title": v.ChapterTitle,
	} {
		sv, err := qdrant.NewValue(s)
		if err != nil {
			return nil, err
		}
		payload[k] = sv
	}

	return &qdrant.PointStruct{
		Id:      qdrant.NewIDNum(v.ID),
		Vectors: qdrant.NewVectors(v.Vector...),
		Payload: payload,
	}, nil
}

// scopeCode 把 scope 字符串(public/private)映射为整数,便于向量库过滤。
// 非 private 一律视为 public(0),与既有 ScopePublic 常量语义一致。
func scopeCode(scope string) int64 {
	if scope == ScopePrivate {
		return 1
	}
	return 0
}

// UpsertVectors 批量写入。
func UpsertVectors(ctx context.Context, items []QdrantVector) error {
	if len(items) == 0 {
		return nil
	}
	points := make([]*qdrant.PointStruct, 0, len(items))
	for _, v := range items {
		p, err := toPointStruct(v)
		if err != nil {
			return err
		}
		points = append(points, p)
	}
	wait := true
	_, err := QdrantClient.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collectionName(),
		Wait:           &wait,
		Points:         points,
	})
	return err
}

// QdrantSearchHit 检索命中结果。
type QdrantSearchHit struct {
	Content      string
	Score        float32
	FileID       uint64
	VersionMd5   string
	ChunkIndex   int
	ChapterTitle string
	Payload      map[string]interface{}
}

// searchFilter 构造过滤：强制 tenant + latest；可见性平面：ownerUserID>0 时= 公库
// OR 本人私有库(pt_scope=1 AND pt_owner=ownerUserID)，ownerUserID=0 时=仅公库（保守，
// 无明确身份的情况下不许看任何私库）；可选 file_id in（must 内进一步限定）。
func searchFilter(tenantID, ownerUserID uint64, fileIDs []uint64) *qdrant.Filter {
	must := []*qdrant.Condition{
		intFieldCond("tenant_id", int64(tenantID)),
		boolFieldCond("latest", true),
	}
	if len(fileIDs) > 0 {
		must = append(must, intsFieldCond("file_id", fileIDs))
	}

	// 可见平面 = 库里可见(scope=public) 或 scope=private 且 owner=ownerUserID
	must = append(must, visibilityCond(ownerUserID))
	return &qdrant.Filter{Must: must}
}

// visibilityCond 构造"该检索身份能看到的点"条件：
//   - ownerUserID>0：pt_scope=0(公库) 与 (pt_scope=1 且 pt_owner=ownerUserID) 取 OR；
//   - ownerUserID==0：仅 pt_scope=0(公库)——避免无明确身份检索他人私库。
//
// OR 在 Qdrant 中用一个"只含 Should 的子 filter"表示（纯 Should → 至少命中其一，
// 即逻辑 OR），via NewFilterAsCondition 把它作为一个 must 项整体嵌入。
func visibilityCond(ownerUserID uint64) *qdrant.Condition {
	publicCond := intFieldCond("pt_scope", 0)
	if ownerUserID == 0 {
		return publicCond
	}
	// scope=private 且 owner=me（同样用嵌套 filter 表达 AND）
	privateCond := qdrant.NewFilterAsCondition(&qdrant.Filter{
		Must: []*qdrant.Condition{
			intFieldCond("pt_scope", 1),
			intFieldCond("pt_owner", int64(ownerUserID)),
		},
	})
	return qdrant.NewFilterAsCondition(&qdrant.Filter{
		Should: []*qdrant.Condition{publicCond, privateCond},
	})
}

func intFieldCond(key string, val int64) *qdrant.Condition {
	return &qdrant.Condition{ConditionOneOf: &qdrant.Condition_Field{Field: &qdrant.FieldCondition{
		Key:   key,
		Match: &qdrant.Match{MatchValue: &qdrant.Match_Integer{Integer: val}},
	}}}
}

func boolFieldCond(key string, val bool) *qdrant.Condition {
	return &qdrant.Condition{ConditionOneOf: &qdrant.Condition_Field{Field: &qdrant.FieldCondition{
		Key:   key,
		Match: &qdrant.Match{MatchValue: &qdrant.Match_Boolean{Boolean: val}},
	}}}
}

func intsFieldCond(key string, vals []uint64) *qdrant.Condition {
	ids := make([]int64, len(vals))
	for i, v := range vals {
		ids[i] = int64(v)
	}
	return &qdrant.Condition{ConditionOneOf: &qdrant.Condition_Field{Field: &qdrant.FieldCondition{
		Key: key,
		Match: &qdrant.Match{MatchValue: &qdrant.Match_Integers{Integers: &qdrant.RepeatedIntegers{Integers: ids}}},
	}}}
}

// SearchVectors 检索：强制 tenant + latest，并按检索身份 ownerUserID 限定可见平面
// （ownerUserID>0 时可见=公库 OR 本人私库；0=仅公库）；可选 fileIDs 进一步限定。
func SearchVectors(ctx context.Context, query []float32, tenantID, ownerUserID uint64, topK int, fileIDs ...uint64) ([]QdrantSearchHit, error) {
	limit := uint64(topK)
	req := &qdrant.QueryPoints{
		CollectionName: collectionName(),
		Query:          qdrant.NewQueryNearest(qdrant.NewVectorInput(query...)),
		Limit:          &limit,
		Filter:         searchFilter(tenantID, ownerUserID, fileIDs),
		WithPayload:    qdrant.NewWithPayload(true),
		WithVectors:    qdrant.NewWithVectors(false),
	}
	resp, err := QdrantClient.Query(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("向量检索失败: %w", err)
	}
	hits := make([]QdrantSearchHit, 0, len(resp))
	for _, scored := range resp {
		p := structToMap(scored.GetPayload())
		content, _ := p["content"].(string)
		fileID, _ := p["file_id"].(int64)
		versionMd5, _ := p["version_md5"].(string)
		chunkIndex, _ := p["chunk_index"].(int64)
		chapterTitle, _ := p["chapter_title"].(string)
		hits = append(hits, QdrantSearchHit{
			Content:      content,
			Score:        scored.GetScore(),
			FileID:       uint64(fileID),
			VersionMd5:   versionMd5,
			ChunkIndex:   int(chunkIndex),
			ChapterTitle: chapterTitle,
			Payload:      p,
		})
	}
	return hits, nil
}

func structToMap(payload map[string]*qdrant.Value) map[string]interface{} {
	out := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		out[k] = valueToInterface(v)
	}
	return out
}

func valueToInterface(v *qdrant.Value) interface{} {
	switch val := v.GetKind().(type) {
	case *qdrant.Value_StringValue:
		return val.StringValue
	case *qdrant.Value_IntegerValue:
		return val.IntegerValue
	case *qdrant.Value_DoubleValue:
		return val.DoubleValue
	case *qdrant.Value_BoolValue:
		return val.BoolValue
	default:
		return nil
	}
}
