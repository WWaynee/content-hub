import { useState } from 'react'
import { Outlet, NavLink, useNavigate } from 'react-router-dom'

export default function Layout() {
  const navigate = useNavigate()
  const [scope, setScope] = useState<'public' | 'private'>('private')

  const logout = () => {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/login')
  }

  return (
    <div style={{ display: 'flex', height: '100vh' }}>
      {/* 左侧导航 */}
      <aside style={{ width: 200, borderRight: '1px solid #e5e7eb', padding: 16, display: 'flex', flexDirection: 'column' }}>
        <h2 style={{ margin: '0 0 24px' }}>content-hub</h2>
        <nav style={{ display: 'flex', flexDirection: 'column', gap: 8, flex: 1 }}>
          <NavLink to="/workspaces" style={navStyle}>工作区</NavLink>
          <NavLink to="/knowledge" style={navStyle}>知识库</NavLink>
        </nav>
        <button onClick={logout} style={{ padding: 8, cursor: 'pointer' }}>退出登录</button>
      </aside>

      {/* 主区域 */}
      <main style={{ flex: 1, overflow: 'auto', padding: 16 }}>
        <Outlet context={{ scope, setScope }} />
      </main>
    </div>
  )
}

function navStyle({ isActive }: { isActive: boolean }) {
  return {
    padding: '10px 12px',
    borderRadius: 6,
    textDecoration: 'none',
    color: isActive ? '#fff' : '#333',
    background: isActive ? '#1d4ed8' : 'transparent',
  }
}
