#!/usr/bin/env bash
# content-hub 阶段3 账号鉴权冒烟自测
# 依赖：API 已在 8181 运行。用法：bash scripts/smoke_phase3.sh
set -u

BASE=http://127.0.0.1:8181
PASS=0; FAIL=0

check() {
  local name="$1"; local expect="$2"; local actual="$3"
  if [ "$actual" = "$expect" ]; then
    echo "PASS  $name"; PASS=$((PASS+1))
  else
    echo "FAIL  $name  (期望=$expect 实际=$actual)"; FAIL=$((FAIL+1))
  fi
}

# 提取 json 字段（极简：按 key 取引号值）
jget() { python3 -c "import sys,json;d=json.load(sys.stdin);print(d$1)"; }

# 1. 无 token 访问私有接口 -> 401
code=$(curl -s -o /dev/null -w "%{http_code}" $BASE/api/user/profile)
check "无token访问私有接口返回401" "401" "$code"

# 2. 错误密码登录 -> 401
code=$(curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/api/user/login \
  -H 'Content-Type: application/json' -d '{"tenant_id":1,"username":"adminA","password":"x"}')
check "错误密码登录返回401" "401" "$code"

# 3. 跨租户登录(租户2无adminA) -> 401
code=$(curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/api/user/login \
  -H 'Content-Type: application/json' -d '{"tenant_id":2,"username":"adminA","password":"pass123456"}')
check "跨租户用户名登录返回401" "401" "$code"

# 4. adminA 登录拿 token
TS=$(curl -s -X POST $BASE/api/user/login -H 'Content-Type: application/json' \
  -d '{"tenant_id":1,"username":"adminA","password":"pass123456"}' | jget '["data"]["token"]')
code=$(curl -s -o /dev/null -w "%{http_code}" $BASE/api/user/profile -H "Authorization: Bearer $TS")
check "admin带token访问profile返回200" "200" "$code"

# 5. member 登录(租户1 worker1)，越权注册工作人员 -> body code=403 (业务拒绝, HTTP 200)
TSW=$(curl -s -X POST $BASE/api/user/login -H 'Content-Type: application/json' \
  -d '{"tenant_id":1,"username":"worker1","password":"pass123456"}' | jget '["data"]["token"]')
resp=$(curl -s -X POST $BASE/api/user/register \
  -H "Authorization: Bearer $TSW" -H 'Content-Type: application/json' -d '{"username":"w2","password":"pass123456"}')
rb=$(echo "$resp" | jget '["code"]')
check "member越权注册工作人员 body.code=403" "403" "$rb"

# 6. 冒名 token 篡改（非法 token）-> 401
code=$(curl -s -o /dev/null -w "%{http_code}" $BASE/api/user/profile -H "Authorization: Bearer bad.token.here")
check "非法token返回401" "401" "$code"

echo ""
echo "通过 $PASS 项，失败 $FAIL 项"
[ "$FAIL" -eq 0 ]
