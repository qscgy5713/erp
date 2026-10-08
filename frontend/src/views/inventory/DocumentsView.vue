<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  inventoryApi,
  type DocStatus,
  type StockDocType,
  type StockDocumentRow,
} from '@/api/inventory'
import { masterdataApi, type Warehouse } from '@/api/masterdata'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { statusLabels } from '@/utils/docstate'
import DocStatusTag from '@/components/DocStatusTag.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = computed(() => auth.can('inventory.stock.write'))
const { handle } = useApiError()

const typeLabels: Record<StockDocType, string> = {
  adjustment: '調整',
  transfer: '調撥',
  count: '盤點',
}

const query = reactive({
  doc_type: '' as StockDocType | '',
  status: ((route.query.status as string | undefined) ?? '') as DocStatus | '',
  warehouse_id: null as number | null,
  keyword: '',
  range: null as [string, string] | null,
  page: 1,
  size: 20,
})
const rows = ref<StockDocumentRow[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)
const warehouses = ref<Warehouse[]>([])

async function load() {
  loading.value = true
  try {
    const { range, ...rest } = query
    const res = await inventoryApi.documents({
      ...rest,
      keyword: rest.keyword.trim(),
      from: range?.[0],
      to: range?.[1],
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

function open(row: StockDocumentRow) {
  router.push({ name: 'inventory-document', params: { id: row.id } })
}

function create(type: StockDocType) {
  router.push({ name: 'inventory-document-new', query: { type } })
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
        v-model="query.doc_type"
        clearable
        placeholder="類型"
        style="width: 100px"
        @change="search"
      >
        <el-option v-for="(label, v) in typeLabels" :key="v" :label="label" :value="v" />
      </el-select>
      <el-select
        v-model="query.status"
        clearable
        placeholder="狀態"
        style="width: 110px"
        @change="search"
      >
        <el-option v-for="(label, v) in statusLabels" :key="v" :label="label" :value="v" />
      </el-select>
      <el-select
        v-model="query.warehouse_id"
        clearable
        placeholder="倉庫"
        style="width: 140px"
        @change="search"
      >
        <el-option
          v-for="w in warehouses"
          :key="w.id"
          :label="`${w.code} ${w.name}`"
          :value="w.id"
        />
      </el-select>
      <el-date-picker
        v-model="query.range"
        type="daterange"
        value-format="YYYY-MM-DD"
        start-placeholder="開始日期"
        end-placeholder="結束日期"
        style="width: 240px"
        @change="search"
      />
      <el-input
        v-model="query.keyword"
        placeholder="單號"
        clearable
        style="width: 160px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button @click="search">查詢</el-button>
      <span class="spacer" />
      <template v-if="canWrite">
        <el-button type="primary" @click="create('adjustment')">新增調整單</el-button>
        <el-button type="primary" @click="create('transfer')">新增調撥單</el-button>
        <el-button type="primary" @click="create('count')">新增盤點單</el-button>
      </template>
    </div>

    <el-table v-loading="loading" :data="rows" border highlight-current-row @row-dblclick="open">
      <el-table-column label="單號" width="160">
        <template #default="{ row }">
          <el-button link type="primary" @click="open(row)">{{ row.doc_no }}</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="doc_date" label="日期" width="110" />
      <el-table-column label="類型" width="80">
        <template #default="{ row }">{{ typeLabels[row.doc_type as StockDocType] }}</template>
      </el-table-column>
      <el-table-column label="倉庫" min-width="160">
        <template #default="{ row }">
          {{ row.warehouse_name
          }}<span v-if="row.to_warehouse_name"> → {{ row.to_warehouse_name }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="line_count" label="明細" width="70" align="right" />
      <el-table-column label="狀態" width="100">
        <template #default="{ row }"><DocStatusTag :status="row.status" /></template>
      </el-table-column>
      <el-table-column prop="created_by_name" label="建立者" width="110" />
      <el-table-column prop="note" label="備註" min-width="160" show-overflow-tooltip />
    </el-table>
    <div class="pager">
      <el-pagination
        v-model:current-page="query.page"
        v-model:page-size="query.size"
        :total="meta.total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        @current-change="load"
        @size-change="search"
      />
    </div>
  </div>
</template>
