import { useState } from 'react'
import { Outlet, useNavigate } from 'react-router-dom'
import {
  Layout as ALayout,
  Menu,
  Button,
  Typography,
  Avatar,
  Dropdown,
  Space,
  Modal,
  App,
  Form,
  Input,
  Tag,
  Tooltip,
} from 'antd'
import {
  FileTextOutlined,
  DatabaseOutlined,
  LogoutOutlined,
  UserOutlined,
  PlusOutlined,
  SunOutlined,
  MoonOutlined,
  TeamOutlined,
  BookOutlined,
} from '@ant-design/icons'
import api from '../api'
import { useTheme } from '../theme'

const { Sider, Content, Header } = ALayout

export default function Layout() {
  const navigate = useNavigate()
  const { message } = App.useApp()
  const { mode, toggle } = useTheme()
  const [scope, setScope] = useState<'public' | 'private'>('private')
  const [selected, setSelected] = useState(() => {
    const p = location.pathname
    if (p.startsWith('/knowledge')) return 'knowledge'
    if (p.startsWith('/manual')) return 'manual'
    return 'workspaces'
  })

  // 用户信息（从 localStorage 读取登录时存的 user）
  let user: any = null
  try {
    user = JSON.parse(localStorage.getItem('user') || 'null')
  } catch {
    user = null
  }
  const username = user?.username || ''
  const isAdmin = user?.role === 'admin'

  // 新增用户弹窗
  const [memberOpen, setMemberOpen] = useState(false)
  const [memberForm] = Form.useForm()
  const [creating, setCreating] = useState(false)

  const logout = () => {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/login')
  }

  const createMember = async () => {
    const v = await memberForm.validateFields()
    setCreating(true)
    try {
      await api.post('/user/register', {
        username: v.username,
        password: v.password,
      })
      message.success(`已新增账号「${v.username}」`)
      setMemberOpen(false)
      memberForm.resetFields()
    } catch (e: any) {
      message.error(e.message || '新增失败')
    } finally {
      setCreating(false)
    }
  }

  const menuItems = [
    { key: 'workspaces', icon: <FileTextOutlined />, label: '工作区' },
    { key: 'knowledge', icon: <DatabaseOutlined />, label: '知识库' },
    { key: 'manual', icon: <BookOutlined />, label: '用户手册' },
  ]

  const userMenu = {
    items: [
      isAdmin
        ? { key: 'add', icon: <PlusOutlined />, label: '新增账号' }
        : null,
      { key: 'logout', icon: <LogoutOutlined />, label: '退出登录' },
    ].filter(Boolean) as any[],
    onClick: ({ key }: { key: string }) => {
      if (key === 'add') setMemberOpen(true)
      if (key === 'logout') logout()
    },
  }

  return (
    <ALayout style={{ minHeight: '100vh' }}>
      <Sider
        theme="dark"
        width={220}
        style={{
          background: 'linear-gradient(180deg, #141a2e 0%, #1b2140 100%)',
          boxShadow: '2px 0 16px rgba(0,0,0,0.20)',
          zIndex: 10,
        }}
      >
        <div
          style={{
            height: 64,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: 10,
            borderBottom: '1px solid rgba(255,255,255,0.08)',
          }}
        >
          <img
            src="/favicon.svg"
            alt="icon"
            style={{ width: 28, height: 28, borderRadius: 6, display: 'block', flexShrink: 0 }}
          />
          <Typography.Title level={4} style={{ color: '#fff', margin: 0, fontWeight: 700, whiteSpace: 'nowrap' }}>
            政企内容运营平台
          </Typography.Title>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[selected]}
          items={menuItems}
          style={{ background: 'transparent' }}
          onClick={({ key }) => {
            setSelected(key as string)
            navigate(key === 'knowledge' ? '/knowledge' : key === 'manual' ? '/manual' : '/workspaces')
          }}
        />
      </Sider>
      <ALayout
        style={{
          background: 'transparent',
          minWidth: 0,
        }}
      >
        <Header
          style={{
            background: 'var(--panel-bg)',
            backdropFilter: 'blur(8px)',
            borderBottom: '1px solid var(--panel-border)',
            padding: '0 24px',
            height: 64,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'flex-end',
            gap: 12,
          }}
        >
          <Space size="middle" align="center">
            {isAdmin && (
              <Tag color="gold" style={{ marginRight: 0 }}>
                管理员
              </Tag>
            )}
            <Tooltip title="切换主题">
              <Button
                type="text"
                shape="circle"
                icon={mode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
                onClick={toggle}
              />
            </Tooltip>
            {isAdmin && (
              <Button icon={<TeamOutlined />} onClick={() => setMemberOpen(true)}>
                新增账号
              </Button>
            )}
            <Dropdown menu={userMenu} placement="bottomRight">
              <Space style={{ cursor: 'pointer', alignItems: 'center' }}>
                <Avatar style={{ background: 'var(--accent-grad)' }} icon={<UserOutlined />} />
                <span style={{ color: 'var(--text-strong)', fontWeight: 600 }}>{username || '未登录'}</span>
              </Space>
            </Dropdown>
          </Space>
        </Header>
        <Content style={{ padding: 24, overflow: 'auto' }}>
          <div className="app-card" style={{ padding: 24, minHeight: '100%' }}>
            <Outlet context={{ scope, setScope }} />
          </div>
        </Content>
      </ALayout>

      <Modal
        title="新增账号（本租户普通账号）"
        open={memberOpen}
        onOk={createMember}
        confirmLoading={creating}
        onCancel={() => setMemberOpen(false)}
        okText="创建"
        cancelText="取消"
      >
        <Form form={memberForm} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item
            label="用户名"
            name="username"
            rules={[{ required: true, min: 2, message: '请输入用户名（至少2位）' }]}
          >
            <Input placeholder="新账号用户名" prefix={<UserOutlined />} />
          </Form.Item>
          <Form.Item
            label="密码"
            name="password"
            rules={[{ required: true, min: 6, message: '请输入密码（至少6位）' }]}
          >
            <Input.Password placeholder="设置初始密码" />
          </Form.Item>
        </Form>
      </Modal>
    </ALayout>
  )
}
