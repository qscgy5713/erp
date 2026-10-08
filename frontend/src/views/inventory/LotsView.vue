<script setup lang="ts">
// 批號庫存:各批號在各倉庫的現有量與效期;可篩選即將到期 / 已過期,並追溯批號從哪裡進、出到哪裡
import { onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  expiryStatusLabels,
  inventoryApi,
  type ExpiryStatus,
  type LotBalance,
  type LotLedger,
} from '@/api/inventory'
import { masterdataApi, type Warehouse } from '@/api/masterdata'
import type { PageMeta } from '@/api/http'
import { useApiError } from '@/composables/useApiError'

const { handle } = useApiError()
const route = useRoute()
const query = reactive({
  warehouse_id: null as number | null,
  keyword: '',
  // 儀表板的效期警示連結帶 ?expiry=expired / expiring
  expiry: (['expired', 'expiring'].includes(String(route.query.expiry))
    ? route.query.expiry
    : '') as '' | 'expired' | 'expiring',
  days: 30,
  include_zero: false,
  page: 1,
  size: 50,
})
const rows = ref<LotBalance[]>([])
const meta = ref<PageMeta>({ page: 1, size: 50, total: 0 })
const loading = ref(false)
const warehouses = ref<Warehouse[]>([])

async function load() {
  loading.value = true
  try {
    const res = await inventoryApi.lots({
      ...query,
      keyword: query.keyword.trim(),
      days: query.expiry === 'expiring' ? query.days : undefined,
    })
    rows.value = res.items
    meta.value = res.meta
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

function search() {
  query.page = 1
  load()
}

const statusType: Record<ExpiryStatus, 'danger' | 'warning' | 'success' | 'info'> = {
  expired: 'danger',
  expiring: 'warning',
  ok: 'success',
  none: 'info',
}

// 批號追溯
const trace = reactive({ visible: false, loading: false, data: null as LotLedger | null })
const sourceLabels: Record<string, string> = {
  goods_receipt: '進貨',
  purchase_return: '進貨退出',
  delivery: '出貨',
  sales_return: '銷貨退回',
  stock_adjustment: '庫存調整',
  stock_transfer: '庫存調撥',
  stock_count: '盤點',
  opening_stock: '期初庫存',
}

async function openTrace(row: LotBalance) {
  Object.assign(trace, { visible: true, loading: true, data: null })
  try {
    trace.data = await inventoryApi.lotLedger(row.lot_id)
  } catch (e) {
    handle(e)
    trace.visible = false
  } finally {
    trace.loading = false
  }
}

onMounted(async () => {
  load()
  try {
    warehouses.value = await masterdataApi.warehouses()
  } catch (e) {
    handle(e)
  }
})
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-select
        v-model="query.warehouse_id"
        clearable
        placeholder="全部倉庫"
        style="width: 150px"
        @change="search"
      >
        <el-option
          v-for="w in warehouses"
          :key="w.id"
          :label="`${w.code} ${w.name}`"
          :value="w.id"
        />
      </el-select>
      <el-input
        v-model="query.keyword"
        placeholder="批號、料號或品名"
        clearable
        style="width: 200px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-radio-group v-model="query.expiry" @change="search">
        <el-radio-button value="">全部</el-radio-button>
        <el-radio-button value="expiring">即將到期</el-radio-button>
        <el-radio-button value="expired">已過期</el-radio-button>
      </el-radio-group>
      <template v-if="query.expiry === 'expiring'">
        <el-input-number
          v-model="query.days"
          :min="1"
          :max="3650"
          controls-position="right"
          style="width: 110px"
          @change="search"
        />
        <span>天內到期</span>
      </template>
      <el-checkbox v-model="query.include_zero" @change="search">含已用完的批號</el-checkbox>
      <el-button @click="search">查詢</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border @row-dblclick="openTrace">
      <el-table-column label="批號" width="170">
        <template #default="{ row }">
          <el-button link type="primary" @click="openTrace(row)">{{ row.lot_no }}</el-button>
        </template>
      </el-table-column>
      <el-table-column label="料品" min-width="200">
        <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
      </el-table-column>
      <el-table-column prop="warehouse_name" label="倉庫" width="120" />
      <el-table-column label="現有量" width="130" align="right">
        <template #default="{ row }">{{ row.qty }} {{ row.unit_name }}</template>
      </el-table-column>
      <el-table-column label="效期" width="120">
        <template #default="{ row }">{{ row.expiry_date ?? '—' }}</template>
      </el-table-column>
      <el-table-column label="狀態" width="150">
        <template #default="{ row }">
          <el-tag :type="statusType[row.expiry_status as ExpiryStatus]" size="small">
            {{ expiryStatusLabels[row.expiry_status as ExpiryStatus] }}
          </el-tag>
          <span v-if="row.days_left !== null" class="muted">
            {{ row.days_left >= 0 ? `剩 ${row.days_left} 天` : `逾期 ${-row.days_left} 天` }}
          </span>
        </template>
      </el-table-column>
    </el-table>
    <div class="pager">
      <el-pagination
        v-model:current-page="query.page"
        v-model:page-size="query.size"
        :total="meta.total"
        :page-sizes="[50, 100]"
        layout="total, sizes, prev, pager, next"
        @current-change="load"
        @size-change="search"
      />
    </div>

    <el-drawer
      v-model="trace.visible"
      size="620px"
      :title="trace.data ? `批號追溯 ${trace.data.lot_no}` : '批號追溯'"
    >
      <div v-loading="trace.loading">
        <template v-if="trace.data">
          <p>
            {{ trace.data.item_code }} {{ trace.data.item_name }}
            <span v-if="trace.data.expiry_date" class="muted"
              >效期 {{ trace.data.expiry_date }}</span
            >
          </p>
          <div v-if="trace.data.bins.length" class="mb">
            儲位:
            <el-tag
              v-for="b in trace.data.bins"
              :key="b.bin_code"
              style="margin-right: 6px"
              effect="plain"
            >
              {{ b.bin_code }} × {{ b.qty }}
            </el-tag>
          </div>
          <el-table :data="trace.data.moves" border size="small">
            <el-table-column prop="date" label="日期" width="105" />
            <el-table-column label="來源" min-width="170">
              <template #default="{ row }">
                {{ sourceLabels[row.source_type] ?? row.source_type }} {{ row.source_no }}
                <el-tag v-if="row.is_reversal" size="small" type="info">沖銷</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="partner" label="對象" width="120" show-overflow-tooltip />
            <el-table-column prop="warehouse_name" label="倉庫" width="90" />
            <el-table-column label="異動" width="80" align="right">
              <template #default="{ row }">
                <span :class="{ neg: Number(row.qty) < 0 }">{{ row.qty }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="balance" label="累計" width="80" align="right" />
          </el-table>
        </template>
      </div>
    </el-drawer>
  </div>
</template>

<style scoped>
.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-left: 6px;
}
.neg {
  color: var(--el-color-danger);
}
</style>
