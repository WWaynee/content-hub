import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Login from './pages/Login'
import Layout from './pages/Layout'
import Workspaces from './pages/Workspaces'
import WorkspaceDetail from './pages/WorkspaceDetail'
import Knowledge from './pages/Knowledge'
import Manual from './pages/Manual'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<Layout />}>
          <Route path="/workspaces" element={<Workspaces />} />
          <Route path="/workspaces/:id" element={<WorkspaceDetail />} />
          <Route path="/knowledge" element={<Knowledge />} />
          <Route path="/manual" element={<Manual />} />
        </Route>
        <Route path="*" element={<Navigate to="/workspaces" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
