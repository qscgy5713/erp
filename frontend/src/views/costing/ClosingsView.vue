<script setup lang="ts">
// 月結成本:月加權平均成本、銷貨成本與存貨盤損益傳票。月結後該月庫存異動鎖定,取消月結可重算
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { costingApi, type Closing, type ItemCost } from '@/api/costing'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { formatDateTime } from '@/utils/format'

const auth = useAuthStore()
const canClose = computed(() => auth.can(['costing.close']))
const { handle } = useApiError()

const rows = ref<Closing[]>([])
const loading = ref(false)
const acting = ref<string | null>(null)

async function load() {
  loading.value = true
  try {
    rows.value = await costingApi.closings()
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

/** 最新一個已月結的月份:只有它可以取消 */
const latestCosted = computed(() => rows.value.find((r) => r.status === 'costed')?.period)

async function run(row: Closing) {
  try {
    await ElMessageBox.confirm(
      `${row.period} 月結後,該月的庫存異動(含進貨 / 出貨過帳與反過帳)會被鎖定,並產生銷貨成本與存貨盤損益傳票。確定月結?`,
      '月結成本',
      { type: 'warning', confirmButtonText: '月結', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  acting.value = row.period
  try {
    await costingApi.run(row.period)
    ElMessage.success('已月結')
    await load()
  } catch (e) {
    handle(e)
  } finally {
    acting.value = null
  }
}

async function cancel(row: Closing) {
  try {
    await ElMessageBox.confirm(
      `取消 ${row.period} 月結會沖銷該月的成本傳票並解除庫存鎖定,之後需重新月結。確定取消?`,
      '取消月結',
      { type: 'warning', confirmButtonText: '取消月結', cancelButtonText: '返回' },
    )
  } catch {
    return
  }
  acting.value = row.period
  try {
    await costingApi.cancel(row.period)
    ElMessage.success('已取消月結')
    await load()
  } catch (e) {
    handle(e)
  } finally {
    acting.value = null
  }
}

// ---- 各料品計算明細 ----
const detail = reactive({
  visible: false,
  period: '',
  keyword: '',
  page: 1,
  size: 20,
  rows: [] as ItemCost[],
  meta: { page: 1, size: 20, total: 0 } as PageMeta,
  loading: false,
})

async function loadDetail() {
  detail.loading = true
  try {
    const res = await costingApi.items(detail.period, {
      keyword: detail.keyword.trim(),
      page: detail.page,
      size: detail.size,
    })
    detail.rows = res.items
    detail.meta = res.meta
  } catch (e) {
    handle(e)
  } finally {
    detail.loading = false
  }
}

function openDetail(row: Closing) {
  Object.assign(detail, { visible: true, period: row.period, keyword: '', page: 1 })
  loadDetail()
}

const money = (v: string | number, places = 0) =>
  Number(v).toLocaleString('zh-TW', {
    minimumFractionDigits: places,
    maximumFractionDigits: Math.max(places, 4),
  })

onMounted(load)
</script>

<template>
  <div>
    <el-alert
      type="info"
      :closable="false"
      show-icon
      class="mb"
      title="月加權平均:平均成本 =(期初金額 + 本月進貨金額)÷(期初數量 + 本月進貨數量);出貨、退回、盤點與調整都以平均成本計價。須依月份順序月結,且只能取消最新的月結。"
    />
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="period" label="月份" width="110" />
      <el-table-column label="狀態" width="100">
        <template #default="{ row }">
          <el-tag :type="row.status === 'costed' ? 'success' : 'info'">
            {{ row.status === 'costed' ? '已月結' : '未月結' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="料品數" width="90" align="right">
        <template #default="{ row }">{{ row.status === 'costed' ? row.item_count : '' }}</template>
      </el-table-column>
      <el-table-column label="銷貨成本" width="130" align="right">
        <template #default="{ row }">{{
          row.status === 'costed' ? money(row.cogs_amount) : ''
        }}</template>
      </el-table-column>
      <el-table-column label="存貨盤損(盈)" width="130" align="right">
        <template #default="{ row }">{{
          row.status === 'costed' ? money(row.adjust_amount) : ''
        }}</template>
      </el-table-column>
      <el-table-column label="期末存貨金額" width="140" align="right">
        <template #default="{ row }">{{
          row.status === 'costed' ? money(row.inventory_value) : ''
        }}</template>
      </el-table-column>
      <el-table-column label="月結時間" width="180">
        <template #default="{ row }">{{ formatDateTime(row.closed_at) }}</template>
      </el-table-column>
      <el-table-column prop="closed_by_name" label="月結者" width="120" />
      <el-table-column label="操作" min-width="220">
        <template #default="{ row }">
          <template v-if="row.status === 'costed'">
            <el-button link type="primary" @click="openDetail(row)">明細</el-button>
            <el-button
              v-if="canClose && row.period === latestCosted"
              link
              type="danger"
              :loading="acting === row.period"
              @click="cancel(row)"
            >
              取消月結
            </el-button>
          </template>
          <el-button
            v-else-if="canClose"
            link
            type="primary"
            :loading="acting === row.period"
            @click="run(row)"
          >
            月結
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="detail.visible" :title="`${detail.period} 成本計算明細`" width="1100px">
      <div class="page-toolbar">
        <el-input
          v-model="detail.keyword"
          placeholder="料號 / 品名"
          clearable
          style="width: 200px"
          @keyup.enter="((detail.page = 1), loadDetail())"
          @clear="((detail.page = 1), loadDetail())"
        />
        <el-button @click="((detail.page = 1), loadDetail())">查詢</el-button>
        <span class="hint">數量為基本單位;平均成本為每基本單位</span>
      </div>
      <el-table v-loading="detail.loading" :data="detail.rows" border size="small" max-height="460">
        <el-table-column label="料品" min-width="180" fixed>
          <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
        </el-table-column>
        <el-table-column prop="unit_name" label="單位" width="60" />
        <el-table-column label="期初量" width="90" align="right">
          <template #default="{ row }">{{ money(row.opening_qty) }}</template>
        </el-table-column>
        <el-table-column label="期初金額" width="100" align="right">
          <template #default="{ row }">{{ money(row.opening_value) }}</template>
        </el-table-column>
        <el-table-column label="進貨量" width="90" align="right">
          <template #default="{ row }">{{ money(row.purchase_qty) }}</template>
        </el-table-column>
        <el-table-column label="進貨金額" width="100" align="right">
          <template #default="{ row }">{{ money(row.purchase_value) }}</template>
        </el-table-column>
        <el-table-column label="平均成本" width="110" align="right">
          <template #default="{ row }">{{ money(row.avg_cost, 4) }}</template>
        </el-table-column>
        <el-table-column label="銷貨量" width="90" align="right">
          <template #default="{ row }">{{ money(row.sales_qty) }}</template>
        </el-table-column>
        <el-table-column label="銷貨成本" width="100" align="right">
          <template #default="{ row }">{{ money(row.cogs_amount) }}</template>
        </el-table-column>
        <el-table-column label="盤調量" width="90" align="right">
          <template #default="{ row }">{{ money(row.adjust_qty) }}</template>
        </el-table-column>
        <el-table-column label="盤損(盈)" width="100" align="right">
          <template #default="{ row }">{{ money(row.adjust_amount) }}</template>
        </el-table-column>
        <el-table-column label="期末量" width="90" align="right">
          <template #default="{ row }">{{ money(row.closing_qty) }}</template>
        </el-table-column>
        <el-table-column label="期末金額" width="110" align="right">
          <template #default="{ row }">{{ money(row.closing_value) }}</template>
        </el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination
          v-model:current-page="detail.page"
          v-model:page-size="detail.size"
          :total="detail.meta.total"
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @current-change="loadDetail"
          @size-change="((detail.page = 1), loadDetail())"
        />
      </div>
    </el-dialog>
  </div>
</template>

<style scoped>
.mb {
  margin-bottom: 12px;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
