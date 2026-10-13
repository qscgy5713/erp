<script setup lang="ts">
// 在製品清單(D101):進行中的工單已領到在製品倉、還沒耗用的材料
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { productionApi, type WipReportRow } from '@/api/production'
import { useApiError } from '@/composables/useApiError'

const router = useRouter()
const { handle } = useApiError()
const rows = ref<WipReportRow[]>([])
const loading = ref(false)
const total = computed(() => rows.value.reduce((s, r) => s + Number(r.amount), 0))

async function load() {
  loading.value = true
  try {
    rows.value = await productionApi.wipReport()
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
    <p class="hint">
      進行中工單領到在製品倉、尚未完工耗用的材料。金額 = 數量 ×
      最近一次月結的平均成本(從未月結的材料以 0 計),僅供管理參考,不入帳。
    </p>
    <el-table
      v-loading="loading"
      :data="rows"
      border
      show-summary
      :summary-method="() => ['合計', '', '', '', '', '', '', total.toLocaleString('zh-TW')]"
    >
      <el-table-column label="工單" width="170">
        <template #default="{ row }"
          ><el-button
            link
            type="primary"
            @click="router.push({ name: 'work-order', params: { id: row.work_order_id } })"
            >{{ row.doc_no }}</el-button
          ></template
        >
      </el-table-column>
      <el-table-column prop="doc_date" label="日期" width="110" />
      <el-table-column label="成品" min-width="180">
        <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
      </el-table-column>
      <el-table-column label="材料" min-width="200">
        <template #default="{ row }">{{ row.material_code }} {{ row.material_name }}</template>
      </el-table-column>
      <el-table-column label="在製品數量" width="130" align="right">
        <template #default="{ row }">{{ row.qty }} {{ row.unit_name }}</template>
      </el-table-column>
      <el-table-column label="平均成本" width="110" align="right">
        <template #default="{ row }">{{ Number(row.avg_cost).toLocaleString('zh-TW') }}</template>
      </el-table-column>
      <el-table-column label="金額" width="120" align="right">
        <template #default="{ row }">{{ Number(row.amount).toLocaleString('zh-TW') }}</template>
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.hint {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0 0 12px;
}
</style>
