import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import axios from 'axios'
import { Form, Input, Button, Segmented, Typography, App } from 'antd'

export default function Login() {
  const navigate = useNavigate()
  const { message } = App.useApp()
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [loading, setLoading] = useState(false)
  const [form] = Form.useForm()

  const doSubmit = async () => {
    const values = form.getFieldsValue()
    setLoading(true)
    try {
      let data: any
      if (mode === 'login') {
        const resp: any = await axios.post('/api/user/login', {
          username: values.username,
          password: values.password,
        })
        data = resp.data?.data
      } else {
        const resp: any = await axios.post('/api/tenant/register', {
          name: values.reg_name,
          admin_name: values.reg_admin,
          admin_passwd: values.reg_pass,
        })
        data = resp.data?.data
      }
      localStorage.setItem('token', data.token)
      localStorage.setItem('user', JSON.stringify(data.user))
      message.success(mode === 'login' ? '登录成功' : '租户注册成功')
      navigate('/workspaces')
    } catch (e: any) {
      message.error(e.response?.data?.message || e.message || '操作失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--app-bg)', backgroundAttachment: 'fixed', padding: 24 }}>
      <div className="app-card" style={{ width: 420, padding: 32 }}>
        <Typography.Title level={3} style={{ textAlign: 'center', marginTop: 0, fontWeight: 700 }}>
          政企内容运营平台
        </Typography.Title>
        <div style={{ textAlign: 'center', color: 'var(--text-soft)', marginBottom: 20, fontSize: 13 }}>
          {mode === 'login' ? '欢迎回来，请登录' : '注册租户并创建管理员账号'}
        </div>
        <Segmented
          block
          value={mode}
          onChange={(v) => setMode(v as 'login' | 'register')}
          options={[
            { label: '登录', value: 'login' },
            { label: '注册租户', value: 'register' },
          ]}
          style={{ marginBottom: 24 }}
        />
        <Form form={form} layout="vertical" onFinish={doSubmit}>
          {mode === 'login' ? (
            <>
              <Form.Item label="用户名" name="username" rules={[{ required: true, message: '请输入用户名' }]}>
                <Input placeholder="用户名" />
              </Form.Item>
              <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }]}>
                <Input.Password placeholder="密码" />
              </Form.Item>
            </>
          ) : (
            <>
              <Form.Item label="租户名称" name="reg_name" rules={[{ required: true, message: '请输入租户名称' }]}>
                <Input placeholder="单位名称" />
              </Form.Item>
              <Form.Item label="管理员用户名" name="reg_admin" rules={[{ required: true, message: '请输入管理员用户名' }]}>
                <Input placeholder="管理员用户名" />
              </Form.Item>
              <Form.Item label="管理员密码" name="reg_pass" rules={[{ required: true, message: '请输入密码' }]}>
                <Input.Password placeholder="管理员密码" />
              </Form.Item>
            </>
          )}
          <Button type="primary" htmlType="submit" block loading={loading} style={{ marginTop: 8 }}>
            {mode === 'login' ? '登录' : '注册并登录'}
          </Button>
        </Form>
      </div>
    </div>
  )
}
