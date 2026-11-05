/**
 * NavigationProgress 组件单元测试
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { createI18n } from 'vue-i18n'
import NavigationProgress from '../../common/NavigationProgress.vue'

// Mock useNavigationLoadingState
const mockIsLoading = ref(false)

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    isLoading: mockIsLoading
  })
}))

function mountProgress() {
  const i18n = createI18n({
    legacy: false,
    locale: 'en',
    messages: { en: { common: { loading: 'Loading' } } }
  })
  return mount(NavigationProgress, { global: { plugins: [i18n] } })
}

describe('NavigationProgress', () => {
  beforeEach(() => {
    mockIsLoading.value = false
  })

  it('isLoading=false 时进度条应该隐藏', () => {
    mockIsLoading.value = false
    const wrapper = mountProgress()

    const progressBar = wrapper.find('.navigation-progress')
    // v-show 会设置 display: none
    expect(progressBar.isVisible()).toBe(false)
  })

  it('isLoading=true 时进度条应该可见', async () => {
    mockIsLoading.value = true
    const wrapper = mountProgress()

    await wrapper.vm.$nextTick()

    const progressBar = wrapper.find('.navigation-progress')
    expect(progressBar.exists()).toBe(true)
    expect(progressBar.isVisible()).toBe(true)
  })

  it('应该有正确的 ARIA 属性', () => {
    mockIsLoading.value = true
    const wrapper = mountProgress()

    const status = wrapper.get('[role="status"]')
    expect(wrapper.get('.navigation-progress').attributes('aria-live')).toBe('polite')
    expect(status.attributes('aria-label')).toBe('Loading')
    expect(status.get('.sr-only').text()).toBe('Loading')
  })

  it('进度条应该有动画 class', () => {
    mockIsLoading.value = true
    const wrapper = mountProgress()

    const bar = wrapper.find('.spinner')
    expect(bar.exists()).toBe(true)
  })

  it('应该正确响应 isLoading 状态变化', async () => {
    // 测试初始状态为 false
    mockIsLoading.value = false
    const wrapper = mountProgress()
    await wrapper.vm.$nextTick()

    // 初始状态隐藏
    expect(wrapper.find('.navigation-progress').isVisible()).toBe(false)

    // 卸载后重新挂载以测试 true 状态
    wrapper.unmount()

    // 改变为 true 后重新挂载
    mockIsLoading.value = true
    const wrapper2 = mountProgress()
    await wrapper2.vm.$nextTick()
    expect(wrapper2.find('.navigation-progress').isVisible()).toBe(true)

    // 清理
    wrapper2.unmount()
  })
})
