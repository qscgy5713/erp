import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import HomeView from '@/views/HomeView.vue'
import { getHealth } from '@/api/system'
import { ApiRequestError } from '@/api/http'

vi.mock('@/api/system', () => ({ getHealth: vi.fn<typeof getHealth>() }))

const mountView = () => mount(HomeView, { global: { plugins: [ElementPlus] } })

describe('HomeView', () => {
  afterEach(() => vi.resetAllMocks())

  it('API 正常時顯示正常', async () => {
    vi.mocked(getHealth).mockResolvedValue({ status: 'ok' })
    const wrapper = mountView()
    expect(wrapper.text()).toContain('檢查中')
    await flushPromises()
    expect(wrapper.text()).toContain('API 與資料庫正常')
  })

  it('API 失敗時顯示錯誤訊息', async () => {
    vi.mocked(getHealth).mockRejectedValue(new ApiRequestError(503, 'SYS-503', '資料庫無回應'))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('異常:資料庫無回應')
  })
})
