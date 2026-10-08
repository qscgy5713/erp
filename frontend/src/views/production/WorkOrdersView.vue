<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { productionApi, type WorkOrderRow } from '@/api/production'
import type { DocStatus } from '@/api/inventory'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { statusLabels } from '@/utils/docstate'
import DocStatusTag from '@/components/DocStatusTag.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = computed(() => auth.can('production.order.write'))
const { handle } = useApiError()

const query = reactive({
  status: ((route.query.status as string | undefined) ?? '') as DocStatus | '',
  keyword: '',
  range: null as [string, string] | null,
  page: 1,
  size: 20,
})
const rows = ref<WorkOrderRow[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { range, ...rest } = query
    const res = await productionApi.orders({
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

const open = (row: WorkOrderRow) => router.push({ name: 'work-order', params: { id: row.id } })

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-select
        v-model="query.status"
        clearable
        placeholder="狀態"
        style="width: 110px"
        @change="search"
      >
        <el-option v-for="(label, v) in statusLabels" :key="v" :label="label" :value="v" />
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
        placeholder="單號、成品"
        clearable
        style="width: 180px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button @click="search">查詢</el-button>
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="router.push({ name: 'work-order-new' })"
        >新增工單</el-button
      >
    </div>

    <el-table v-loading="loading" :data="rows" border highlight-current-row @row-dblclick="open">
      <el-table-column label="單號" width="160">
        <template #default="{ row }"
          ><el-button link type="primary" @click="open(row)">{{ row.doc_no }}</el-button></template
        >
      </el-table-column>
      <el-table-column prop="doc_date" label="日期" width="110" />
      <el-table-column label="成品" min-width="220">
        <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
      </el-table-column>
      <el-table-column label="完工數量" width="130" align="right">
        <template #default="{ row }">{{ row.plan_qty }} {{ row.unit_name }}</template>
      </el-table-column>
      <el-table-column prop="warehouse_name" label="入庫倉" width="110" />
      <el-table-column label="加工費" width="110" align="right">
        <template #default="{ row }">{{
          Number(row.processing_cost).toLocaleString('zh-TW')
        }}</template>
      </el-table-column>
      <el-table-column prop="due_date" label="預定完工" width="110" />
      <el-table-column label="狀態" width="100">
        <template #default="{ row }"><DocStatusTag :status="row.status" /></template>
      </el-table-column>
      <el-table-column prop="created_by_name" label="建立者" width="110" />
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
