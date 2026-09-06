import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from 'antd'
import RequirementScopeModal from './RequirementScopeModal'

vi.mock('../api', () => ({
  default: {
    get: vi.fn(async (url: string) => {
      if (url.includes('/scope')) return []
      if (url.startsWith('/kbase/all?scope=public')) return { dirs: [], files: [] }
      return {
        dirs: [{ id: 100, name: '招生政策', parent_id: 0 }],
        files: [
          { id: 201, name: '报名条件.md', dir_id: 100 },
          { id: 202, name: '录取规则.md', dir_id: 100 },
        ],
      }
    }),
  },
}))

describe('RequirementScopeModal · 点行选择可用', () => {
  beforeEach(() => vi.clearAllMocks())

  it('点击目录所在行即选中目录及其下文件，显示已选计数', async () => {
    render(
      <App>
        <RequirementScopeModal open requirementId={9} onClose={() => {}} onSaved={() => {}} />
      </App>,
    )
    await waitFor(() => expect(screen.getByText('招生政策')).toBeTruthy())

    // 点击目录所在行（antd Tree 的可点区域是 node-content-wrapper）
    const title = screen.getByText('招生政策')
    const row = title.closest('.ant-tree-node-content-wrapper') as HTMLElement
    expect(row).toBeTruthy()
    fireEvent.click(row)

    // 目录 + 两个子文件共 3 项
    await waitFor(() => expect(screen.getByText(/已选 3 项/)).toBeTruthy())
    const wrapper = row.closest('.ant-tree-treenode') as HTMLElement
    expect(wrapper?.querySelector('.ant-tree-node-selected')).toBeTruthy()
  })
})
