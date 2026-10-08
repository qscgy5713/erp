<script setup lang="ts">
// 儲位庫存:各儲位放了哪些料品、多少(只有啟用儲位的倉庫才有資料)
import { onMounted, reactive, ref } from 'vue'
import { inventoryApi, type BinStock } from '@/api/inventory'
import { masterdataApi, type Bin, type Warehouse } from '@/api/masterdata'
import type { PageMeta } from '@/api/http'
import { useApiError } from '@/composables/useApiError'

const { handle } = useApiError()
const query = reactive({
  warehouse_id: null as number | null,
  bin_id: null as number | null,
  keyword: '',
  include_zero: false,
  page: 1,
  size: 50,
})
const rows = ref<BinStock[]>([])
const meta = ref<PageMeta>({ page: 1, size: 50, total: 0 })
const loading = ref(false)
const warehouses = ref<Warehouse[]>([])
const bins = ref<Bin[]>([])

async function load() {
  loading.value = true
  try {
    const res = await inventoryApi.binStock({ ...query, keyword: query.keyword.trim() })
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

async function onWarehouse() {
  query.bin_id = null
  bins.value = []
  if (query.warehouse_id) {
    try {
      bins.value = await masterdataApi.bins(query.warehouse_id)
    } catch (e) {
      handle(e)
    }
  }
  search()
}

onMounted(async () => {
  load()
  try {
    warehouses.value = (await masterdataApi.warehouses()).filter((w) => w.use_bins)
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
        @change="onWarehouse"
      >
        <el-option
          v-for="w in warehouses"
          :key="w.id"
          :label="`${w.code} ${w.name}`"
          :value="w.id"
        />
      </el-select>
      <el-select
        v-model="query.bin_id"
        clearable
        placeholder="全部儲位"
        style="width: 150px"
        :disabled="!query.warehouse_id"
        @change="search"
      >
        <el-option v-for="b in bins" :key="b.id" :label="b.code" :value="b.id" />
      </el-select>
      <el-input
        v-model="query.keyword"
        placeholder="儲位、料號或品名"
        clearable
        style="width: 200px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-checkbox v-model="query.include_zero" @change="search">含已清空的</el-checkbox>
      <el-button @click="search">查詢</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="warehouse_name" label="倉庫" width="130" />
      <el-table-column label="儲位" width="160">
        <template #default="{ row }"
          >{{ row.bin_code }} <span class="muted">{{ row.bin_name }}</span></template
        >
      </el-table-column>
      <el-table-column label="料品" min-width="220">
        <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
      </el-table-column>
      <el-table-column label="數量" width="140" align="right">
        <template #default="{ row }">{{ row.qty }} {{ row.unit_name }}</template>
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
.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
