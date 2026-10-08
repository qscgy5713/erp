<script setup lang="ts">
// 未出貨清單:已核准訂單中尚未出齊的明細(依資料範圍),可只看逾期
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { salesApi, type UnshippedLine } from '@/api/sales'
import type { PageMeta } from '@/api/http'
import { useApiError } from '@/composables/useApiError'
import PartnerPicker from '@/components/PartnerPicker.vue'

const router = useRouter()
const { handle } = useApiError()

const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const query = reactive({
  customer_id: null as number | null,
  keyword: '',
  overdue: false,
  page: 1,
  size: 50,
})
const rows = ref<UnshippedLine[]>([])
const meta = ref<PageMeta>({ page: 1, size: 50, total: 0 })
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await salesApi.unshipped({
      customer_id: query.customer_id,
      keyword: query.keyword.trim(),
      // 逾期:預定出貨日早於今天
      due_before: query.overdue ? yesterday() : undefined,
      page: query.page,
      size: query.size,
    })
    rows.value = res.items
    meta.value = res.meta
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

function yesterday(): string {
  const d = new Date(`${today()}T00:00:00`)
  d.setDate(d.getDate() - 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function search() {
  query.page = 1
  load()
}

const isOverdue = (row: UnshippedLine) => !!row.delivery_date && row.delivery_date < today()
const qty = (v: string) => Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 4 })

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <div style="width: 200px">
        <PartnerPicker v-model="query.customer_id" kind="customer" @update:model-value="search" />
      </div>
      <el-input
        v-model="query.keyword"
        placeholder="單號 / 客戶單號 / 料號 / 品名"
        clearable
        style="width: 200px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-checkbox v-model="query.overdue" @change="search">只看逾期</el-checkbox>
      <el-button @click="search">查詢</el-button>
      <el-button v-if="query.customer_id" link @click="((query.customer_id = null), search())">
        清除客戶
      </el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border size="small">
      <el-table-column label="訂單" width="160">
        <template #default="{ row }">
          <el-button
            link
            type="primary"
            @click="router.push({ name: 'sales-order', params: { id: row.order_id } })"
          >
            {{ row.doc_no }}
          </el-button>
        </template>
      </el-table-column>
      <el-table-column prop="doc_date" label="訂單日" width="100" />
      <el-table-column label="預定出貨" width="110">
        <template #default="{ row }">
          <span :class="{ overdue: isOverdue(row) }">{{ row.delivery_date }}</span>
        </template>
      </el-table-column>
      <el-table-column label="客戶" min-width="160">
        <template #default="{ row }">{{ row.customer_code }} {{ row.customer_name }}</template>
      </el-table-column>
      <el-table-column label="料品" min-width="200">
        <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
      </el-table-column>
      <el-table-column prop="unit_name" label="單位" width="70" />
      <el-table-column label="訂購量" width="90" align="right">
        <template #default="{ row }">{{ qty(row.qty) }}</template>
      </el-table-column>
      <el-table-column label="已出" width="90" align="right">
        <template #default="{ row }">{{ qty(row.delivered_qty) }}</template>
      </el-table-column>
      <el-table-column label="未出" width="90" align="right">
        <template #default="{ row }">
          <strong>{{ qty(row.remaining_qty) }}</strong>
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
  </div>
</template>

<style scoped>
.overdue {
  color: var(--el-color-danger);
  font-weight: 600;
}
</style>
