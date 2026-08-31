import { useState } from 'react'
import { Outlet, useNavigate } from 'react-router-dom'
import { Layout as ALayout, Menu, Button, Typography } from 'antd'
import {
  FileTextOutlined,
  DatabaseOutlined,
  LogoutOutlined,
} from '@ant-design/icons'

const { Sider, Content, Header } = ALayout

export default function Layout() {
  const navigate = useNavigate()
  const [scope, setScope] = useState<'public' | 'private'>('private')

  const [selected, setSelected] = useState(() =>
    location.pathname.startsWith('/knowledge') ? 'knowledge' : 'workspaces',
  )

  const logout = () => {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/login')
  }

  const menuItems = [
    { key: 'workspaces', icon: <FileTextOutlined />, label: '工作区' },
    { key: 'knowledge', icon: <DatabaseOutlined />, label: '知识库' },
  ]

  return (
    <ALayout style={{ minHeight: '100vh' }}>
      <Sider theme="dark" width={200}>
        <div style={{ height: 56, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Typography.Title level={4} style={{ color: '#fff', margin: 0 }}>
            content-hub
          </Typography.Title>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[selected]}
          items={menuItems}
          onClick={({ key }) => {
            setSelected(key as string)
            navigate(key === 'knowledge' ? '/knowledge' : '/workspaces')
          }}
        />
      </Sider>
      <ALayout>
        <Header
          style={{
            background: '#fff',
            padding: '0 24px',
            display: 'flex',
            justifyContent: 'flex-end',
            alignItems: 'center',
          }}
        >
          <Button icon={<LogoutOutlined />} onClick={logout}>
            退出登录
          </Button>
        </Header>
        <Content style={{ margin: 16, padding: 16, background: '#fff', borderRadius: 8 }}>
          <Outlet context={{ scope, setScope }} />
        </Content>
      </ALayout>
    </ALayout>
  )
}
