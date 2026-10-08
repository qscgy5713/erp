<script setup lang="ts">
// 會計期間:關帳後該期不可過帳 / 反過帳單據或新增傳票;需有「關帳 / 重開」權限
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { glApi, type Period } from '@/api/gl'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { formatDateTime } from '@/utils/format'

const auth = useAuthStore()
const canClose = computed(() => auth.can(['gl.period.close']))
const { handle } = useApiError()

const rows = ref<Period[]>([])
const loading = ref(false)
const acting = ref<string | null>(null)

async function load() {
  loading.value = true
  try {
    rows.value = await glApi.periods()
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

async function change(row: Period, action: 'close' | 'reopen') {
  const label = action === 'close' ? '關帳' : '重開'
  const msg =
    action === 'close'
      ? `${row.period} 關帳後,該期的單據不可過帳 / 反過帳,也不可新增傳票。確定關帳?`
      : `${row.period} 重開後可再異動該期的單據與傳票。確定重開?`
  try {
    await ElMessageBox.confirm(msg, label, {
      type: 'warning',
      confirmButtonText: label,
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  acting.value = row.period
  try {
    await glApi.changePeriod(row.period, action)
    ElMessage.success(`已${label}`)
    await load()
  } catch (e) {
    handle(e)
  } finally {
    acting.value = null
  }
}

onMounted(load)
</script>

<template>
  <div>
    <el-alert
      type="info"
      :closable="false"
      show-icon
      class="mb"
      title="關帳前須先把該期的草稿傳票過帳或作廢。已關帳的期間若要更正,請先重開。"
    />
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="period" label="期間" width="120" />
      <el-table-column label="狀態" width="110">
        <template #default="{ row }">
          <el-tag :type="row.status === 'closed' ? 'danger' : 'success'">
            {{ row.status === 'closed' ? '已關帳' : '開放' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="關帳時間" width="190">
        <template #default="{ row }">{{ formatDateTime(row.closed_at) }}</template>
      </el-table-column>
      <el-table-column prop="closed_by_name" label="關帳者" width="130" />
      <el-table-column v-if="canClose" label="操作" width="120">
        <template #default="{ row }">
          <el-button
            v-if="row.status === 'open'"
            link
            type="danger"
            :loading="acting === row.period"
            @click="change(row, 'close')"
          >
            關帳
          </el-button>
          <el-button
            v-else
            link
            type="primary"
            :loading="acting === row.period"
            @click="change(row, 'reopen')"
          >
            重開
          </el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.mb {
  margin-bottom: 12px;
}
</style>
