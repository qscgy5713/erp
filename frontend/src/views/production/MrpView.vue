<script setup lang="ts">
// 需求計算(MRP):由訂單未出貨、未完工工單領料、安全庫存,扣掉庫存與在途,算出該生產或採購多少;
// 有 BOM 的建議「生產」並往下一階展開材料需求。只是建議,不會自動開單。
import { onMounted, ref } from 'vue'
import { productionApi, type MrpRow } from '@/api/production'
import { useApiError } from '@/composables/useApiError'

const { handle } = useApiError()
const rows = ref<MrpRow[]>([])
const loading = ref(false)
const shortageOnly = ref(true)
const num = (v: string) => Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 4 })

async function load() {
  loading.value = true
  try {
    rows.value = await productionApi.mrp(shortageOnly.value)
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <span class="hint">
        淨需求 = 訂單未出貨 + 未完工工單領料 + 上層建議生產帶下來的需求 + 安全庫存 − 現有庫存 −
        採購在途 − 工單在途。僅供參考,不會自動開單。
      </span>
      <span class="spacer" />
      <el-checkbox v-model="shortageOnly" @change="load">只看需補的料品</el-checkbox>
      <el-button @click="load">重新計算</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" border empty-text="目前沒有需補的料品">
      <el-table-column prop="item_code" label="料號" width="130" fixed />
      <el-table-column prop="item_name" label="品名" min-width="160" />
      <el-table-column prop="unit_name" label="單位" width="70" />
      <el-table-column prop="level" label="階層" width="70" align="center" />
      <el-table-column label="訂單未出" width="100" align="right">
        <template #default="{ row }">{{ num(row.sales_demand) }}</template>
      </el-table-column>
      <el-table-column label="工單領料" width="100" align="right">
        <template #default="{ row }">{{ num(row.wo_demand) }}</template>
      </el-table-column>
      <el-table-column label="上層帶下" width="100" align="right">
        <template #default="{ row }">{{ num(row.plan_demand) }}</template>
      </el-table-column>
      <el-table-column label="安全庫存" width="100" align="right">
        <template #default="{ row }">{{ num(row.safety_stock) }}</template>
      </el-table-column>
      <el-table-column label="現有庫存" width="100" align="right">
        <template #default="{ row }">{{ num(row.on_hand) }}</template>
      </el-table-column>
      <el-table-column label="採購在途" width="100" align="right">
        <template #default="{ row }">{{ num(row.incoming_po) }}</template>
      </el-table-column>
      <el-table-column label="工單在途" width="100" align="right">
        <template #default="{ row }">{{ num(row.incoming_wo) }}</template>
      </el-table-column>
      <el-table-column label="淨需求" width="100" align="right">
        <template #default="{ row }">{{ num(row.net) }}</template>
      </el-table-column>
      <el-table-column label="建議" width="130" fixed="right">
        <template #default="{ row }">
          <el-tag v-if="row.action === 'make'" type="warning"
            >生產 {{ num(row.suggested_qty) }}</el-tag
          >
          <el-tag v-else-if="row.action === 'buy'" type="danger"
            >採購 {{ num(row.suggested_qty) }}</el-tag
          >
          <span v-else class="muted">夠用</span>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>
