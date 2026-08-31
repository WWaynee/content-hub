import { StrictMode, useEffect } from 'react'
import { createRoot } from 'react-dom/client'
import { App as AntdApp, ConfigProvider, theme as antdTheme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import './index.css'
import { ThemeProvider, useTheme } from './theme'
import App from './App.tsx'

// 依据主题模式设置 antd 算法 + 根节点 data-theme（驱动 CSS 变量）
function ThemedApp() {
  const { mode } = useTheme()
  useEffect(() => {
    document.documentElement.setAttribute('data-theme', mode)
  }, [mode])
  const algorithm =
    mode === 'dark' ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm,
        token: {
          colorPrimary: '#4f6ef5',
          borderRadius: 8,
          colorBgLayout: 'transparent',
        },
      }}
    >
      <AntdApp>
        <App />
      </AntdApp>
    </ConfigProvider>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <ThemedApp />
    </ThemeProvider>
  </StrictMode>,
)
