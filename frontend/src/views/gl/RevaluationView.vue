<script setup lang="ts">
// 外幣期末重估:以月底匯率重估外幣應收 / 應付,差額記未實現兌換損益;下次重估時自動沖回上期
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { glApi, type RevalHistory, type RevalPreview } from '@/api/gl'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const canClose = computed(() => auth.can(['gl.period.close']))
const { handle } = useApiError()

const now = new Date()
const lastMonth = new Date(now.getFullYear(), now.getMonth() - 1, 1)
const period = ref(
  `${lastMonth.getFullYear()}-${String(lastMonth.getMonth() + 1).padStart(2, '0')}`,
)
const data = ref<RevalPreview>({ history: [] })
const loading = ref(false)
const acting = ref(false)

const money = (v: string | undefined) =>
  Number(v ?? 0).toLocaleString('zh-TW', { maximumFractionDigits: 4 })
const latestActive = computed(() => data.value.history.find((h) => h.status === 'active'))

async function load() {
  loading.value = true
  try {
    data.value = await glApi.revaluation(period.value)
  } catch (e) {
    data.value = { history: data.value.history }
    handle(e)
  } finally {
    loading.value = false
  }
}

async function run() {
  try {
    await ElMessageBox.confirm(
      `執行 ${period.value} 外幣期末重估?若有上一次重估,會一併沖回(沖銷傳票日期為本期第一天)。`,
      '外幣期末重估',
      { type: 'warning' },
    )
  } catch {
    return
  }
  acting.value = true
  try {
    await glApi.runRevaluation(period.value)
    ElMessage.success('已完成重估')
    await load()
  } catch (e) {
    handle(e)
  } finally {
    acting.value = false
  }
}

async function undo(h: RevalHistory) {
  try {
    await ElMessageBox.confirm(`撤銷 ${h.period} 的重估?`, '撤銷重估', { type: 'warning' })
  } catch {
    return
  }
  acting.value = true
  try {
    await glApi.undoRevaluation(h.period)
    ElMessage.success('已撤銷')
    await load()
  } catch (e) {
    handle(e)
  } finally {
    acting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-date-picker
        v-model="period"
        type="month"
        value-format="YYYY-MM"
        :clearable="false"
        @change="load"
      />
      <el-button @click="load">試算</el-button>
      <span class="spacer" />
      <el-button v-if="canClose" type="primary" :loading="acting" @click="run">執行重估</el-button>
    </div>
    <p class="hint">
      以截止日(月底)匯率重估外幣應收、應付的未沖餘額,與帳面本位幣金額的差額記入「未實現兌換損益」。只能依期間順序重估;下一次重估時會自動沖回上一次。
    </p>

    <el-table
      v-loading="loading"
      :data="data.rows ?? []"
      border
      size="small"
      empty-text="沒有外幣未沖餘額"
    >
      <el-table-column prop="currency" label="幣別" width="80" />
      <el-table-column label="匯率" width="100" align="right">
        <template #default="{ row }">{{ Number(row.rate) }}</template>
      </el-table-column>
      <el-table-column label="應收原幣" align="right">
        <template #default="{ row }">{{ money(row.ar_foreign) }}</template>
      </el-table-column>
      <el-table-column label="應收帳面" align="right">
        <template #default="{ row }">{{ money(row.ar_carrying) }}</template>
      </el-table-column>
      <el-table-column label="應收重估" align="right">
        <template #default="{ row }">{{ money(row.ar_revalued) }}</template>
      </el-table-column>
      <el-table-column label="應付原幣" align="right">
        <template #default="{ row }">{{ money(row.ap_foreign) }}</template>
      </el-table-column>
      <el-table-column label="應付帳面" align="right">
        <template #default="{ row }">{{ money(row.ap_carrying) }}</template>
      </el-table-column>
      <el-table-column label="應付重估" align="right">
        <template #default="{ row }">{{ money(row.ap_revalued) }}</template>
      </el-table-column>
    </el-table>
    <div v-if="data.rows" class="sum">
      應收差額 {{ money(data.ar_diff) }}、應付差額 {{ money(data.ap_diff) }}
      <strong>
        →
        {{ Number(data.net_gain) >= 0 ? '未實現利益' : '未實現損失' }}
        {{ money(String(Math.abs(Number(data.net_gain)))) }}
      </strong>
    </div>

    <h4>重估紀錄</h4>
    <el-table :data="data.history" border size="small" empty-text="尚未重估過">
      <el-table-column prop="period" label="期間" width="110" />
      <el-table-column label="狀態" width="90">
        <template #default="{ row }">{{ row.status === 'active' ? '有效' : '已撤銷' }}</template>
      </el-table-column>
      <el-table-column label="應收差額" align="right">
        <template #default="{ row }">{{ money(row.ar_diff) }}</template>
      </el-table-column>
      <el-table-column label="應付差額" align="right">
        <template #default="{ row }">{{ money(row.ap_diff) }}</template>
      </el-table-column>
      <el-table-column v-if="canClose" label="操作" width="90">
        <template #default="{ row }">
          <el-button
            v-if="row.id === latestActive?.id"
            link
            type="danger"
            :disabled="acting"
            @click="undo(row)"
            >撤銷</el-button
          >
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin: 4px 0 10px;
}
.sum {
  margin: 10px 0;
}
</style>
