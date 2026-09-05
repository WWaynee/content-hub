// vitest 冒烟的环境垫片：jsdom 没有 antd 需要的浏览器 API，补齐后组件才能渲染。
import '@testing-library/jest-dom/vitest'

// antd 响应式/媒体查询依赖 window.matchMedia
if (typeof window !== 'undefined' && !window.matchMedia) {
  ;(window as any).matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })
}

// antd（layout/trigger 等）依赖 ResizeObserver
class ROStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (typeof window !== 'undefined' && !(window as any).ResizeObserver) {
  ;(window as any).ResizeObserver = ROStub
}
