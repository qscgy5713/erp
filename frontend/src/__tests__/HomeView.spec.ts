import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import HomeView from '@/views/HomeView.vue'
import { getHealth } from '@/api/system'
import { dashboardApi, type Dashboard } from '@/api/dashboard'
import { ApiRequestError } from '@/api/http'

vi.mock('@/api/system', () => ({ getHealth: vi.fn<typeof getHealth>() }))
vi.mock('@/api/dashboard', () => ({ dashboardApi: { get: vi.fn<() => Promise<Dashboard>>() } }))
const push = vi.fn<(path: string) => void>()
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))

const empty: Dashboard = { date: '2026-10-08', pending: [] }
const mountView = () => mount(HomeView, { global: { plugins: [ElementPlus, createPinia()] } })

describe('HomeView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(dashboardApi.get).mockResolvedValue(empty)
  })
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

  it('只顯示後端回傳的卡片,沒有資料時提示', async () => {
    vi.mocked(getHealth).mockResolvedValue({ status: 'ok' })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('目前沒有可顯示的資料')
    expect(wrapper.text()).not.toContain('庫存警示')
    expect(wrapper.text()).not.toContain('應收帳款')
  })

  it('依權限顯示銷售、待審、庫存警示、應收應付,並可點待審前往', async () => {
    vi.mocked(getHealth).mockResolvedValue({ status: 'ok' })
    vi.mocked(dashboardApi.get).mockResolvedValue({
      date: '2026-10-08',
      sales: {
        today: '2000',
        today_count: 2,
        month: '12345',
        month_count: 9,
        scope_label: '全公司',
        daily: [{ date: '2026-10-08', amount: '2000' }],
      },
      pending: [
        {
          key: 'purchase_orders',
          label: '採購單',
          count: 3,
          path: '/purchase/orders?status=pending',
        },
      ],
      low_stock: {
        count: 1,
        items: [
          { id: 1, code: 'P1', name: '筆', unit_name: '個', safety_stock: '100', total: '40' },
        ],
      },
      receivable: { open_amount: '5000', overdue_amount: '1200', overdue_count: 2 },
      payable: {
        open_amount: '800',
        overdue_amount: '0',
        overdue_count: 0,
        due_soon_amount: '300',
      },
    })
    const wrapper = mountView()
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('12,345')
    expect(text).toContain('全公司')
    expect(text).toContain('3 筆待審')
    expect(text).toContain('P1 筆')
    expect(text).toContain('40 / 100 個')
    expect(text).toContain('1,200')
    expect(text).toContain('已逾期(2 筆)')
    expect(text).toContain('7 日內到期')
    await wrapper
      .findAll('button')
      .find((b) => b.text().includes('3 筆待審'))!
      .trigger('click')
    expect(push).toHaveBeenCalledWith('/purchase/orders?status=pending')
  })

  it('儀表板載入失敗時顯示錯誤,其餘頁面仍可用', async () => {
    vi.mocked(getHealth).mockResolvedValue({ status: 'ok' })
    vi.mocked(dashboardApi.get).mockRejectedValue(
      new ApiRequestError(500, 'SYS-500', '系統發生錯誤'),
    )
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('儀表板載入失敗:系統發生錯誤')
  })

  it('上個月尚未月結時提醒', async () => {
    vi.mocked(getHealth).mockResolvedValue({ status: 'ok' })
    vi.mocked(dashboardApi.get).mockResolvedValue({
      ...empty,
      costing: { last_closing: '2026-07', current_month: '2026-10' },
    })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('上個月(2026-09)尚未月結成本')
  })
})
