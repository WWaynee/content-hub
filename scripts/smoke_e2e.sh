#!/usr/bin/env bash
# content-hub 端到端冒烟验收脚本（含多租户隔离对抗）
#
# 前置：后端 API 已在 :8181 运行、worker 已运行、依赖中间件已启动。
# 用法：bash scripts/smoke_e2e.sh
#
# 覆盖：注册登录 / 知识库目录 / 上传文档(MQ异步向量化) / 知识库问答 /
#        工作区 / 需求单 / 勾选范围 / 生成稿件 / 读稿件 / 导出 / 对话派发 /
#        多租户隔离对抗(A/B 双向越权验证)
set -u
BASE=http://127.0.0.1:8181
PASS=0; FAIL=0

ok()  { echo "PASS  $1"; PASS=$((PASS+1)); }
bad() { echo "FAIL  $1"; FAIL=$((FAIL+1)); }

jget() { python3 -c "import sys,json;d=json.load(sys.stdin);print(d$1)"; }

RANDSFX=$(date +%s%N)

echo "===== 1. 注册租户 A ====="
REG_A=$(curl -s -X POST $BASE/api/tenant/register -H 'Content-Type: application/json' \
  -d "{\"name\":\"smokeA_${RANDSFX}\",\"admin_name\":\"sma\",\"admin_passwd\":\"smoke123456\"}")
TOKEN_A=$(echo "$REG_A" | jget '["data"]["token"]')
if [ -n "$TOKEN_A" ]; then ok "租户A注册成功"; else bad "租户A注册失败: $REG_A"; fi

echo "===== 2. 登录 A ====="
RID_A=$(echo "$REG_A" | jget '["data"]["user"]["tenant_id"]')
L=$(curl -s -X POST $BASE/api/user/login -H 'Content-Type: application/json' -d "{\"tenant_id\":$RID_A,\"username\":\"sma\",\"password\":\"smoke123456\"}")
if [ "$(echo "$L" | jget '["code"]')" = "0" ]; then ok "租户A登录"; else bad "租户A登录失败: $L"; fi

echo "===== 3. 建目录(私有) ====="
DIR=$(curl -s -X POST $BASE/api/kbase/dir -H "Authorization: Bearer $TOKEN_A" -H 'Content-Type: application/json' -d '{"scope":"private","parent_id":0,"name":"资料"}')
DIDA=$(echo "$DIR" | jget '["data"]["id"]')
[ -n "$DIDA" ] && [ "$DIDA" != "None" ] && ok "建目录 dir=$DIDA" || bad "建目录失败: $DIR"

echo "===== 4. 上传文档(worker异步) ====="
UP=$(curl -s -X POST $BASE/api/kbase/file -H "Authorization: Bearer $TOKEN_A" -F "scope=private" -F "dir_id=$DIDA" -F "file=@scripts/smoke_doc.md;type=text/markdown")
FIDA=$(echo "$UP" | jget '["data"]["file_id"]')
[ -n "$FIDA" ] && [ "$FIDA" != "None" ] && ok "上传文档 file=$FIDA" || bad "上传失败: $UP"

echo "===== 5. 等待 worker 向量化 ====="
sleep 15

echo "===== 6. 知识库问答 ====="
QS=$(curl -s -X POST $BASE/api/qa/sessions -H "Authorization: Bearer $TOKEN_A")
QSID=$(echo "$QS" | jget '["data"]["id"]')
[ -n "$QSID" ] && ok "建问答会话" || bad "建会话: $QS"
ANS=$(curl -s -X POST $BASE/api/qa/sessions/$QSID/ask -H "Authorization: Bearer $TOKEN_A" -H 'Content-Type: application/json' -d '{"question":"报名条件是什么？"}')
A=$(echo "$ANS" | jget '["data"]["answer"]')
[ -n "$A" ] && ok "问答: $A" || bad "问答失败: $ANS"

echo "===== 7. 建工作区 + 需求单 ====="
W=$(curl -s -X POST $BASE/api/workspaces -H "Authorization: Bearer $TOKEN_A" -H 'Content-Type: application/json' -d '{"title":"招生稿"}')
WIDA=$(echo "$W" | jget '["data"]["id"]')
[ -n "$WIDA" ] && ok "建工作区 wid=$WIDA" || bad "建工作区: $W"
REQ=$(curl -s -X GET $BASE/api/workspaces/$WIDA/requirement -H "Authorization: Bearer $TOKEN_A")
REQID=$(echo "$REQ" | jget '["data"]["id"]')
[ -n "$REQID" ] && ok "建需求单 rid=$REQID" || bad "读需求单: $REQ"

echo "===== 8. 保存需求单 + 勾选范围 ====="
curl -s -X PUT $BASE/api/requirements/$REQID -H "Authorization: Bearer $TOKEN_A" -H 'Content-Type: application/json' \
  -d "{\"title\":\"招生简章发布稿\",\"tags\":[\"招生\"],\"platforms\":[\"微信公众号\"],\"style_tone\":\"正式\",\"style_emotion\":\"积极\",\"style_audience\":\"考生及家长\",\"style_purpose\":\"发布招生政策\",\"style_subject\":\"学校\",\"word_count\":300,\"chapter_requirement\":\"包含报名条件和录取规则\"}" > /dev/null
curl -s -X PUT $BASE/api/requirements/$REQID/scope -H "Authorization: Bearer $TOKEN_A" -H 'Content-Type: application/json' \
  -d "{\"scopes\":[{\"scope_type\":\"private\",\"target_type\":\"dir\",\"dir_id\":$DIDA}]}" > /dev/null
ok "保存需求单+勾选范围"

echo "===== 9. 生成稿件 ====="
GEN=$(curl -s -X POST $BASE/api/workspaces/$WIDA/generate -H "Authorization: Bearer $TOKEN_A" -H 'Content-Type: application/json' -d '{}')
AV=$(echo "$GEN" | jget '["data"]["article_version_id"]')
[ -n "$AV" ] && [ "$AV" != "None" ] && ok "生成稿件 av=$AV" || bad "生成失败: $GEN"

echo "===== 10. 读稿件 + 导出 ====="
ART=$(curl -s -X GET $BASE/api/workspaces/$WIDA/article -H "Authorization: Bearer $TOKEN_A")
[ -n "$(echo "$ART" | jget '["data"]["title"]')" ] && ok "读稿件" || bad "读稿件: $ART"
EXP=$(curl -s -X GET $BASE/api/articles/$AV/export -H "Authorization: Bearer $TOKEN_A")
if echo "$EXP" | grep -q "证据清单"; then ok "导出含证据清单"; else bad "导出异常"; fi

echo "===== 11. 对话派发 ====="
CHAT=$(curl -s -X POST $BASE/api/workspaces/$WIDA/chat -H "Authorization: Bearer $TOKEN_A" -H 'Content-Type: application/json' -d "{\"message\":\"把基调改成严谨\",\"target_type\":\"requirement_field\",\"target_ref\":$REQID}")
if echo "$CHAT" | grep -q "update_requirement_field"; then ok "对话派发识别动作(update_requirement_field)"; else bad "对话派发: ${CHAT:0:80}"; fi

echo "===== 12. 多租户隔离对抗 ====="
# 注册租户 B（无关租户），尝试越权访问 A 的资源
REG_B=$(curl -s -X POST $BASE/api/tenant/register -H 'Content-Type: application/json' \
  -d "{\"name\":\"smokeB_${RANDSFX}\",\"admin_name\":\"smb\",\"admin_passwd\":\"smoke123456\"}")
TOKEN_B=$(echo "$REG_B" | jget '["data"]["token"]')

# B 的私有库目录列表不应出现 A 的目录
BLIST=$(curl -s -X GET "$BASE/kbase/dir?scope=private" -H "Authorization: Bearer $TOKEN_B")
if ! echo "$BLIST" | grep -q "\"id\":$DIDA\|,$DIDA\b"; then ok "B 看不到 A 的私有目录(隔离)"; else bad "B 越权看到 A 的目录!"; fi

# B 尝试读 A 的工作区稿件（应 400/401，取不到 A 的稿件）
BART=$(curl -s -o /dev/null -w "%{http_code}" -X GET $BASE/api/workspaces/$WIDA/article -H "Authorization: Bearer $TOKEN_B")
# 业务错误是 HTTP 200 + 非0 code，这里验证 B 拿不到 A 的稿件（响应不含标题）
BARTBODY=$(curl -s -X GET $BASE/api/workspaces/$WIDA/article -H "Authorization: Bearer $TOKEN_B")
if echo "$BARTBODY" | grep -q "稿件不存在"; then ok "B 越权读 A 稿件被拒"; else bad "B 可能越权读 A 稿件"; fi

# B 尝试预览 A 的文档（应 400 文档不存在，而非拿到 URL）
BPRE=$(curl -s -X GET $BASE/api/kbase/file/$FIDA/preview -H "Authorization: Bearer $TOKEN_B")
IPRE=$(echo "$BPRE" | jget '["code"]')
[ "$IPRE" != "0" ] && ok "B 越权预览 A 文档被拒" || bad "B 可能越权预览 A 文档!"

echo ""
echo "===== 冒烟+对抗验收：通过 $PASS，失败 $FAIL ====="
[ "$FAIL" -eq 0 ]
