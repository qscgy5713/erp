<script setup lang="ts">
// 對帳檢查:子帳(庫存、應收、應付)與總帳是否一致、庫存現有量與流水帳是否一致、傳票是否平衡
import { onMounted, ref } from 'vue'
import { costingApi, type Reconcile } from '@/api/costing'
import { useApiError } from '@/composables/useApiError'
import { formatDateTime } from '@/utils/format'

const { handle } = useApiError()
const result = ref<Reconcile | null>(null)
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    result.value = await costingApi.reconcile()
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

const statusTag = {
  ok: { label: '正常', type: 'success' },
  warn: { label: '提醒', type: 'warning' },
  error: { label: '異常', type: 'danger' },
} as const

const money = (v: string | null) =>
  v === null ? '' : Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 4 })

onMounted(load)
</script>

<template>
  <div v-loading="loading">
    <div class="page-toolbar">
      <el-button type="primary" @click="load">重新檢查</el-button>
      <span v-if="result" class="hint">檢查時間 {{ formatDateTime(result.checked_at) }}</span>
      <span class="spacer" />
      <el-tag v-if="result" :type="result.ok ? 'success' : 'danger'" size="large">
        {{ result.ok ? '全部通過' : '有異常項目' }}
      </el-tag>
    </div>
    <el-table :data="result?.checks ?? []" border>
      <el-table-column label="檢查項目" min-width="280" prop="label" />
      <el-table-column label="結果" width="90">
        <template #default="{ row }">
          <el-tag :type="statusTag[row.status as keyof typeof statusTag].type">
            {{ statusTag[row.status as keyof typeof statusTag].label }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="子帳 / 計算值" width="140" align="right">
        <template #default="{ row }">{{ money(row.subledger) }}</template>
      </el-table-column>
      <el-table-column label="總帳 / 對照值" width="140" align="right">
        <template #default="{ row }">{{ money(row.gl) }}</template>
      </el-table-column>
      <el-table-column label="差異" width="120" align="right">
        <template #default="{ row }">
          <span :class="{ bad: row.status === 'error' }">{{ money(row.diff) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="說明" min-width="320" prop="message" />
    </el-table>
    <p class="hint">
      金額以本位幣計。差異容許每筆帳款 / 料品 1 元的四捨五入;M6
      之前已過帳、沒有傳票的單據會造成差異,匯入期初餘額後即可消除。
    </p>
  </div>
</template>

<style scoped>
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.bad {
  color: var(--el-color-danger);
  font-weight: 600;
}
</style>
