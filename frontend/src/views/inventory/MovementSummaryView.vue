<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { inventoryApi, type MovementSummary } from '@/api/inventory'
import { masterdataApi, type Warehouse } from '@/api/masterdata'
import type { PageMeta } from '@/api/http'
import { useApiError } from '@/composables/useApiError'
import ItemLedgerDrawer from '@/components/ItemLedgerDrawer.vue'

const { handle } = useApiError()
const now = new Date()
const ym = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`
const lastDay = new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate()

const query = reactive({
  range: [`${ym}-01`, `${ym}-${lastDay}`] as [string, string],
  warehouse_id: null as number | null,
  keyword: '',
  page: 1,
  size: 50,
})
const rows = ref<MovementSummary[]>([])
const meta = ref<PageMeta>({ page: 1, size: 50, total: 0 })
const loading = ref(false)
const warehouses = ref<Warehouse[]>([])

async function load() {
  loading.value = true
  try {
    const { range, ...rest } = query
    const res = await inventoryApi.movementSummary({
      ...rest,
      keyword: rest.keyword.trim(),
      from: range[0],
      to: range[1],
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

const ledger = reactive({ visible: false, itemId: null as number | null, title: '' })

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
      <el-date-picker
        v-model="query.range"
        type="daterange"
        value-format="YYYY-MM-DD"
        :clearable="false"
        style="width: 240px"
        @change="search"
      />
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
        placeholder="料號或品名"
        clearable
        style="width: 180px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button @click="search">查詢</el-button>
    </div>
    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="依單據日期統計:期初 + 收 − 發 = 期末(基本單位)。反過帳的沖銷會與原分錄互相抵銷。"
      class="mb"
    />
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column label="料號" width="140">
        <template #default="{ row }">
          <el-button
            link
            type="primary"
            @click="
              Object.assign(ledger, {
                visible: true,
                itemId: row.item_id,
                title: `${row.item_code} ${row.item_name}`,
              })
            "
          >
            {{ row.item_code }}
          </el-button>
        </template>
      </el-table-column>
      <el-table-column prop="item_name" label="品名" min-width="160" />
      <el-table-column prop="unit_name" label="單位" width="70" />
      <el-table-column prop="opening_qty" label="期初" width="110" align="right" />
      <el-table-column prop="in_qty" label="收" width="110" align="right" />
      <el-table-column prop="out_qty" label="發" width="110" align="right" />
      <el-table-column prop="closing_qty" label="期末" width="110" align="right" />
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
      :warehouse-id="query.warehouse_id"
      :from="query.range[0]"
      :to="query.range[1]"
    />
  </div>
</template>

<style scoped>
.mb {
  margin-bottom: 12px;
}
</style>
