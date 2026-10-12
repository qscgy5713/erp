<script setup lang="ts">
// 待我核准:待核准的單據中,現在輪到我核准的那些;點單號進入單據核准
import { onMounted, ref } from 'vue'
import { inboxApi, inboxRoutes, type InboxRow } from '@/api/approval'
import { useApiError } from '@/composables/useApiError'
import { formatDateTime } from '@/utils/format'

const { handle } = useApiError()
const rows = ref<InboxRow[]>([])
const loading = ref(false)
const money = (v: string) => Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 4 })

async function load() {
  loading.value = true
  try {
    rows.value = await inboxApi.list()
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
      <span class="hint">只列出現在輪到你核准的單據(依核准權限、資料範圍與多層簽核流程判斷)。</span>
      <span class="spacer" />
      <el-button @click="load">重新整理</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" border empty-text="目前沒有待你核准的單據">
      <el-table-column prop="doc_label" label="類型" width="170" />
      <el-table-column label="單號" width="180">
        <template #default="{ row }">
          <router-link :to="{ name: inboxRoutes[row.doc_type], params: { id: row.doc_id } }">{{
            row.doc_no
          }}</router-link>
        </template>
      </el-table-column>
      <el-table-column prop="doc_date" label="日期" width="120" />
      <el-table-column prop="partner_name" label="對象 / 品項" min-width="160" />
      <el-table-column label="金額" width="150" align="right">
        <template #default="{ row }">{{ row.currency }} {{ money(row.amount) }}</template>
      </el-table-column>
      <el-table-column label="簽核層級" width="170">
        <template #default="{ row }">
          <template v-if="row.total_steps > 1"
            >第 {{ row.step }}/{{ row.total_steps }} 層(<span>{{ row.step_role }}</span
            >)</template
          >
          <template v-else>單層核准</template>
        </template>
      </el-table-column>
      <el-table-column label="送審時間" width="170">
        <template #default="{ row }">{{ formatDateTime(row.submitted_at) }}</template>
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
