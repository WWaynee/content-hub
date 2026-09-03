package coordinator

import (
	"testing"

	"github.com/WWaynee/content-hub/storage/model"
)

// P05 验收单元：run 状态机的合法迁移表。
// running→success/failed/awaiting_human/cancelled 全部可达；非法转移或从终态出发即被拒绝。
func TestRunStateTransitions(t *testing.T) {
	// 合法迁移
	legal := map[model.RunStatus][]model.RunStatus{
		model.RunRunning:       {model.RunSuccess, model.RunFailed, model.RunAwaitingHuman, model.RunCancelled},
		model.RunAwaitingHuman: {model.RunRunning, model.RunFailed, model.RunCancelled},
	}
	for from, tos := range legal {
		for _, to := range tos {
			if !CanTransition(from, to) {
				t.Errorf("应允许合法迁移 %s→%s", from, to)
			}
		}
	}

	// 非法：终态不能动、不可逆重建
	illegal := [][2]model.RunStatus{
		{model.RunSuccess, model.RunRunning},
		{model.RunSuccess, model.RunFailed},
		{model.RunFailed, model.RunSuccess},
		{model.RunCancelled, model.RunSuccess},
		{model.RunSuccess, model.RunAwaitingHuman},
	}
	for _, p := range illegal {
		if CanTransition(p[0], p[1]) {
			t.Errorf("应拒绝非法迁移 %s→%s", p[0], p[1])
		}
	}

	// 终态集合稳定（三个终态都不应可再动）
	for _, term := range []model.RunStatus{model.RunSuccess, model.RunFailed, model.RunCancelled} {
		if len(ValidTransition[string(term)]) != 0 {
			t.Errorf("终态 %s 不应存在出边", term)
		}
	}
}
