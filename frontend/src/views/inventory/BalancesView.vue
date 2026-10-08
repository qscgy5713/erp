<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { inventoryApi, type Balance } from '@/api/inventory'
import { masterdataApi, type ItemCategory, type Warehouse } from '@/api/masterdata'
import type { PageMeta } from '@/api/http'
import { useApiError } from '@/composables/useApiError'
import { buildTree } from '@/utils/tree'
import ItemLedgerDrawer from '@/components/ItemLedgerDrawer.vue'

const { handle } = useApiError()
const query = reactive({
  warehouse_id: null as number | null,
  category_id: null as number | null,
  keyword: '',
  nonzero: true,
  below_safety: false,
  page: 1,
  size: 50,
})
const rows = ref<Balance[]>([])
const meta = ref<PageMeta>({ page: 1, size: 50, total: 0 })
const loading = ref(false)
const warehouses = ref<Warehouse[]>([])
const categories = ref<ItemCategory[]>([])
const categoryTree = computed(() => buildTree(categories.value))

async function load() {
  loading.value = true
  try {
    const res = await inventoryApi.balances({ ...query, keyword: query.keyword.trim() })
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

// 異動明細:預設本月
const ledger = reactive({
  visible: false,
  itemId: null as number | null,
  title: '',
  warehouseId: null as number | null,
})
const monthRange = (() => {
  const d = new Date()
  const ym = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
  const last = new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate()
  return [`${ym}-01`, `${ym}-${last}`] as const
})()

function openLedger(row: Balance) {
  Object.assign(ledger, {
    visible: true,
    itemId: row.item_id,
    title: `${row.item_code} ${row.item_name} @ ${row.warehouse_name}`,
    warehouseId: row.warehouse_id,
  })
}

onMounted(async () => {
  load()
  try {
    ;[warehouses.value, categories.value] = await Promise.all([
      masterdataApi.warehouses(),
      masterdataApi.categories(),
    ])
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
      <el-tree-select
        v-model="query.category_id"
        :data="categoryTree"
        :props="{ label: 'name', children: 'children' }"
        node-key="id"
        check-strictly
        clearable
        placeholder="分類(含下層)"
        style="width: 170px"
        @change="search"
      />
      <el-input
        v-model="query.keyword"
        placeholder="料號或品名"
        clearable
        style="width: 180px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-checkbox v-model="query.nonzero" @change="search">只顯示有庫存</el-checkbox>
      <el-checkbox v-model="query.below_safety" @change="search">低於安全庫存</el-checkbox>
      <el-button @click="search">查詢</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" border @row-dblclick="openLedger">
      <el-table-column label="料號" width="140">
        <template #default="{ row }">
          <el-button link type="primary" @click="openLedger(row)">{{ row.item_code }}</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="item_name" label="品名" min-width="160" />
      <el-table-column prop="item_spec" label="規格" min-width="120" show-overflow-tooltip />
      <el-table-column prop="warehouse_name" label="倉庫" width="120" />
      <el-table-column label="現有量" width="120" align="right">
        <template #default="{ row }">
          <span :class="{ neg: Number(row.qty) < 0 }">{{ row.qty }}</span> {{ row.unit_name }}
        </template>
      </el-table-column>
      <el-table-column label="全倉合計 / 安全庫存" width="170" align="right">
        <template #default="{ row }">
          <el-tag v-if="row.below_safety" type="danger" size="small">不足</el-tag>
          {{ row.item_total }} / {{ row.safety_stock }}
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
    <ItemLedgerDrawer
      v-model="ledger.visible"
      :item-id="ledger.itemId"
      :title="ledger.title"
      :warehouse-id="ledger.warehouseId"
      :from="monthRange[0]"
      :to="monthRange[1]"
    />
  </div>
</template>

<style scoped>
.neg {
  color: var(--el-color-danger);
}
</style>
