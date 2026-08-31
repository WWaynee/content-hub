import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import axios from 'axios'

export default function Login() {
  const navigate = useNavigate()
  const [mode, setMode] = useState<'login' | 'register'>('login')

  // 登录字段
  const [tenantId, setTenantId] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  // 注册字段
  const [regName, setRegName] = useState('')
  const [regAdmin, setRegAdmin] = useState('')
  const [regPass, setRegPass] = useState('')

  const [err, setErr] = useState('')

  const doLogin = async () => {
    setErr('')
    try {
      const data: any = await axios.post('/api/user/login', {
        tenant_id: Number(tenantId),
        username,
        password,
      })
      localStorage.setItem('token', data.token)
      localStorage.setItem('user', JSON.stringify(data.user))
      navigate('/workspaces')
    } catch (e: any) {
      setErr(e.response?.data?.message || e.message || '登录失败')
    }
  }

  const doRegister = async () => {
    setErr('')
    try {
      const data: any = await axios.post('/api/tenant/register', {
        name: regName,
        admin_name: regAdmin,
        admin_passwd: regPass,
      })
      localStorage.setItem('token', data.token)
      localStorage.setItem('user', JSON.stringify(data.user))
      navigate('/workspaces')
    } catch (e: any) {
      setErr(e.response?.data?.message || e.message || '注册失败')
    }
  }

  return (
    <div style={{ maxWidth: 420, margin: '80px auto', padding: 24 }}>
      <h2>content-hub 登录</h2>
      <div style={{ marginBottom: 16 }}>
        <button onClick={() => setMode('login')} disabled={mode === 'login'}>登录</button>
        <button onClick={() => setMode('register')} disabled={mode === 'register'} style={{ marginLeft: 8 }}>注册租户</button>
      </div>

      {mode === 'login' ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <input placeholder="租户 ID" value={tenantId} onChange={(e) => setTenantId(e.target.value)} />
          <input placeholder="用户名" value={username} onChange={(e) => setUsername(e.target.value)} />
          <input placeholder="密码" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
          <button onClick={doLogin}>登录</button>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <input placeholder="租户名称" value={regName} onChange={(e) => setRegName(e.target.value)} />
          <input placeholder="管理员用户名" value={regAdmin} onChange={(e) => setRegAdmin(e.target.value)} />
          <input placeholder="管理员密码" type="password" value={regPass} onChange={(e) => setRegPass(e.target.value)} />
          <button onClick={doRegister}>注册并登录</button>
        </div>
      )}
      {err && <p style={{ color: 'red' }}>{err}</p>}
    </div>
  )
}
