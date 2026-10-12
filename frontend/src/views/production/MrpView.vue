<script setup lang="ts">
// 需求計算(MRP):由訂單未出貨、未完工工單領料、安全庫存,扣掉庫存與在途,算出該生產或採購多少;
// 有 BOM 的建議「生產」並往下一階展開材料需求。依交期分週計算(展開可看各週明細),
// 建議量依料品的最小量與批量進位,並算出最晚下單日。只是建議,不會自動開單。
import { onMounted, ref } from 'vue'
import { productionApi, type MrpRow } from '@/api/production'
import { useApiError } from '@/composables/useApiError'

const { handle } = useApiError()
const rows = ref<MrpRow[]>([])
const loading = ref(false)
const shortageOnly = ref(true)
const weeks = ref(8)
const num = (v: string) => Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 4 })

async function load() {
  loading.value = true
  try {
    rows.value = await productionApi.mrp(shortageOnly.value, weeks.value)
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
        依預定交期(訂單出貨日、採購預定交貨日、工單預定完工日)分週計算;沒填日期或已逾期的算在第 1
        週。預計庫存低於安全庫存就建議補貨。僅供參考,不會自動開單。
      </span>
      <span class="spacer" />
      <span>計算</span>
      <el-input-number v-model="weeks" :min="1" :max="26" size="small" @change="load" />
      <span>週</span>
      <el-checkbox v-model="shortageOnly" @change="load">只看需補的料品</el-checkbox>
      <el-button @click="load">重新計算</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" border empty-text="目前沒有需補的料品">
      <el-table-column type="expand">
        <template #default="{ row }">
          <el-table :data="row.buckets" size="small" border class="detail">
            <el-table-column label="週開始日" prop="start" width="120" />
            <el-table-column label="毛需求" align="right">
              <template #default="{ row: b }">{{ num(b.gross) }}</template>
            </el-table-column>
            <el-table-column label="預計入庫" align="right">
              <template #default="{ row: b }">{{ num(b.receipts) }}</template>
            </el-table-column>
            <el-table-column label="建議補貨" align="right">
              <template #default="{ row: b }">
                <b v-if="Number(b.planned) > 0">{{ num(b.planned) }}</b>
                <span v-else class="muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="期末預計庫存" align="right">
              <template #default="{ row: b }">{{ num(b.projected) }}</template>
            </el-table-column>
          </el-table>
          <div class="muted">
            前置時間 {{ row.lead_time_days }} 天、最小量 {{ num(row.min_order_qty) }}、批量倍數
            {{ num(row.order_multiple) }}(到「MRP 計畫參數」設定)
          </div>
        </template>
      </el-table-column>
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
      <el-table-column label="最晚下單日" width="130">
        <template #default="{ row }">
          <template v-if="row.release_by">
            {{ row.release_by }}
            <el-tooltip content="前置時間來不及:即使今天下單也會晚" :disabled="!row.late">
              <el-tag v-if="row.late" type="danger" size="small">來不及</el-tag>
            </el-tooltip>
          </template>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="建議" width="190" fixed="right">
        <template #default="{ row }">
          <el-tag v-if="row.action === 'make'" type="warning"
            >生產 {{ num(row.suggested_qty) }}</el-tag
          >
          <el-tag v-else-if="row.action === 'buy'" type="danger"
            >採購 {{ num(row.suggested_qty) }}</el-tag
          >
          <span v-else class="muted">夠用</span>
          <el-popover v-if="row.substitutes.length" placement="left" :width="300" trigger="hover">
            <template #reference>
              <el-tag type="success" effect="plain" class="sub-tag">可替代</el-tag>
            </template>
            <div v-for="s in row.substitutes" :key="s.item_id" class="sub-line">
              {{ s.item_code }} {{ s.item_name }}:剩餘 {{ num(s.free_qty) }},可頂替
              {{ num(s.covers) }} {{ row.unit_name }}(1:{{ num(s.ratio) }})
            </div>
            <div class="muted">僅供參考,不影響上方建議量</div>
          </el-popover>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.detail {
  margin: 8px 16px;
  width: calc(100% - 32px);
}
.muted {
  margin: 0 16px 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.sub-tag {
  margin-left: 6px;
}
.sub-line {
  margin-bottom: 4px;
}
</style>
