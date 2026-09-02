import { useEffect, useMemo, useState } from 'react'
import { Modal, Tree, Segmented, App, Button, Space, Typography, Empty } from 'antd'
import type { DataNode } from 'antd/es/tree'
import api from '../api'
import type { KbaseDir, KbaseFile } from '../types'

// 需求单「引用资料范围」勾选弹窗。
// 私有库与公有库**可同时**勾选（两个库各自独立维护勾选集合），目录=递归含其下全部文件。
type ScopeType = 'private' | 'public'
type TreeNode = DataNode & { isDir?: boolean }

interface LoadedScope {
  dirs: KbaseDir[]
  files: KbaseFile[]
}

interface Props {
  open: boolean
  requirementId: number
  onClose: () => void
  onSaved: () => void
}

// 每个 scope 独立维护一组选中的 key（d_/f_ 前缀对应当库 dir/file id，库间隔离、可并存）
type CheckedByScope = Record<ScopeType, string[]>

const NEW_CHECKED: CheckedByScope = { private: [], public: [] }

export default function RequirementScopeModal({ open, requirementId, onClose, onSaved }: Props) {
  const { message } = App.useApp()
  const [tab, setTab] = useState<ScopeType>('private')
  const [data, setData] = useState<Partial<Record<ScopeType, LoadedScope>>>({})
  const [checked, setChecked] = useState<CheckedByScope>(NEW_CHECKED)
  const [expanded, setExpanded] = useState<Record<ScopeType, string[]>>({ private: [], public: [] })
  const [saving, setSaving] = useState(false)

  // 打开：拉两库全量数据 + 已选范围（按 scope_type 分别回显）
  useEffect(() => {
    if (!open) return
    setChecked(NEW_CHECKED)
    setExpanded({ private: [], public: [] })
    Promise.all([loadScope('private'), loadScope('public')])
      .then(([priv, pub]) => {
        setData({ private: priv, public: pub })
        return api.get(`/requirements/${requirementId}/scope`) as any
      })
      .then((scopes: any[]) => {
        const next: CheckedByScope = { private: [], public: [] }
        for (const s of scopes || []) {
          const st: ScopeType = s.scope_type === 'public' ? 'public' : 'private'
          const key = s.target_type === 'dir' ? `d_${s.dir_id}` : `f_${s.file_id}`
          // 仅当该 key 确实存在于对应库数据里才回显（避免悬空/串号）
          if (keyBelongs(data[st], key)) next[st].push(key)
        }
        setChecked(next)
      })
      .catch((e: any) => message.error(e.message || '加载资料失败'))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, requirementId])

  const loadScope = async (scope: ScopeType): Promise<LoadedScope> => {
    const r = (await api.get(`/kbase/all?scope=${scope}`)) as any
    return { dirs: r?.dirs || [], files: r?.files || [] }
  }

  const treeNodes = useMemo(() => {
    const d = data[tab]
    if (!d) return []
    return buildNodes(d.dirs, d.files)
  }, [data, tab])

  // 目录数据就位后，把当前 tab 尚未记录的目录全部展开
  useEffect(() => {
    const d = data[tab]
    const curExpanded = expanded[tab] || []
    if (d && d.dirs.length && curExpanded.length === 0) {
      setExpanded((pre) => ({ ...pre, [tab]: d.dirs.map((x) => `d_${x.id}`) }))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, tab])

  // antd Tree 事件回调的参数带其内部 Key 类型，这里宽松接收并按需转字符串
  const onCheck = (keys: unknown) => {
    const arr = keys as (string | number | { checked: unknown })[]
    // checkStrictly 下返回纯数组；防御个别形态返回对象
    const list = Array.isArray(arr) ? arr : []
    setChecked((pre) => ({ ...pre, [tab]: (list as (string | number)[]).map(String) }))
  }
  const onExpand = (keys: unknown) => {
    const arr = keys as (string | number)[]
    setExpanded((pre) => ({ ...pre, [tab]: Array.isArray(arr) ? arr.map(String) : [] }))
  }

  const doSave = async () => {
    setSaving(true)
    try {
      const scopes: any[] = []
      ;(['private', 'public'] as ScopeType[]).forEach((st) => {
        ;(checked[st] || []).forEach((k) => {
          if (k.startsWith('d_')) {
            const id = Number(k.slice(2))
            if (id) scopes.push({ scope_type: st, target_type: 'dir', dir_id: id, file_id: 0 })
          } else if (k.startsWith('f_')) {
            const id = Number(k.slice(2))
            if (id) scopes.push({ scope_type: st, target_type: 'file', dir_id: 0, file_id: id })
          }
        })
      })
      await api.put(`/requirements/${requirementId}/scope`, { scopes })
      message.success('引用范围已保存')
      onSaved()
      onClose()
    } catch (e: any) {
      message.error(e.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      title="选择引用资料范围（可同时勾选私/公两个库）"
      open={open}
      width={660}
      onCancel={onClose}
      destroyOnClose
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={saving} onClick={doSave}>
            保存范围
          </Button>
        </Space>
      }
    >
      <Space direction="vertical" style={{ width: '100%' }} size={8}>
        <Segmented
          value={tab}
          onChange={(v) => setTab(v as ScopeType)}
          options={[
            { label: '私有库', value: 'private' },
            { label: '公有库', value: 'public' },
          ]}
        />
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          两个库可分别勾选、互不影响，可同时保存。勾选目录表示引用该目录及其下所有文件（含子目录）；也可展开目录精确勾选单个文件。
          {tab === 'private' ? '【当前正在配置：私有库】' : '【当前正在配置：公有库】'}
        </Typography.Text>
        <div style={{ maxHeight: 420, overflow: 'auto', border: '1px solid var(--panel-border)', borderRadius: 8, padding: 8 }}>
          {treeNodes.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该库暂无内容" />
          ) : (
            <Tree
              checkable
              selectable={false}
              onExpand={onExpand}
              expandedKeys={expanded[tab] || []}
              checkedKeys={checked[tab] || []}
              onCheck={onCheck}
              treeData={treeNodes}
              showIcon
              checkStrictly
            />
          )}
        </div>
      </Space>
    </Modal>
  )
}

function buildNodes(dirs: KbaseDir[], files: KbaseFile[]): TreeNode[] {
  const dirMap = new Map<number, TreeNode>()
  dirs.forEach((d) => {
    dirMap.set(d.id, { key: `d_${d.id}`, title: d.name, isDir: true, children: [] })
  })
  const topFiles: TreeNode[] = []
  files.forEach((f) => {
    const node: TreeNode = { key: `f_${f.id}`, title: f.name, isDir: false }
    if (f.dir_id && dirMap.has(f.dir_id)) {
      dirMap.get(f.dir_id)!.children!.push(node)
    } else {
      topFiles.push(node)
    }
  })
  const roots: TreeNode[] = []
  dirs.forEach((d) => {
    const node = dirMap.get(d.id)!
    if (d.parent_id && dirMap.has(d.parent_id)) {
      ;(dirMap.get(d.parent_id)!.children as TreeNode[]).push(node)
    } else {
      roots.push(node)
    }
  })
  if (topFiles.length) {
    roots.push({ key: `plist`, title: '（根目录文件）', isDir: false, selectable: false, children: topFiles })
  }
  return roots
}

// 判断某个 key 是否属于该库数据集合（用于回显时防悬空）
function keyBelongs(d: LoadedScope | undefined, key: string): boolean {
  if (!d) return false
  const id = Number(key.slice(2))
  if (key.startsWith('d_')) return d.dirs.some((x) => x.id === id)
  if (key.startsWith('f_')) return d.files.some((x) => x.id === id)
  return false
}
