<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { systemApi, type AuditLog } from '@/api/system'
import type { PageMeta } from '@/api/http'
import { useApiError } from '@/composables/useApiError'
import { formatDateTime } from '@/utils/format'
import JsonDiff from '@/components/JsonDiff.vue'

const { handle } = useApiError()

const actionLabels: Record<string, string> = {
  create: '新增',
  update: '修改',
  delete: '刪除',
  login: '登入',
  login_failed: '登入失敗',
  logout: '登出',
  password_change: '變更密碼',
  password_reset: '重設密碼',
  unlock: '解除鎖定',
  token_reuse: '憑證重放',
  submit: '送審',
  reject: '退回',
  approve: '核准',
  unapprove: '取消核准',
  post: '過帳',
  unpost: '反過帳',
  void: '作廢',
}
const entityLabels: Record<string, string> = {
  user: '使用者',
  role: '角色',
  department: '部門',
  doc_number_rule: '單號規則',
  item: '料品',
  item_category: '料品分類',
  unit: '單位',
  warehouse: '倉庫',
  customer: '客戶',
  supplier: '供應商',
  currency: '幣別',
  exchange_rate: '匯率',
  tax_type: '稅別',
  payment_term: '付款條件',
  stock_document: '庫存單據',
}

const query = reactive({
  entity_type: '',
  action: '',
  range: null as [string, string] | null,
  page: 1,
  size: 20,
})
const rows = ref<AuditLog[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await systemApi.auditLogs({
      entity_type: query.entity_type,
      action: query.action,
      from: query.range?.[0],
      to: query.range?.[1],
      page: query.page,
      size: query.size,
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
onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-select
        v-model="query.entity_type"
        clearable
        placeholder="對象"
        style="width: 130px"
        @change="search"
      >
        <el-option v-for="(label, v) in entityLabels" :key="v" :label="label" :value="v" />
      </el-select>
      <el-select
        v-model="query.action"
        clearable
        placeholder="動作"
        style="width: 130px"
        @change="search"
      >
        <el-option v-for="(label, v) in actionLabels" :key="v" :label="label" :value="v" />
      </el-select>
      <el-date-picker
        v-model="query.range"
        type="daterange"
        value-format="YYYY-MM-DD"
        start-placeholder="開始日期"
        end-placeholder="結束日期"
        @change="search"
      />
      <el-button @click="search">查詢</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border row-key="id">
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="detail">
            <JsonDiff v-if="row.before || row.after" :before="row.before" :after="row.after" />
            <p class="muted">IP:{{ row.ip }} ・ Request ID:{{ row.request_id }}</p>
            <p class="muted">{{ row.user_agent }}</p>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="時間" width="180">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="使用者" width="160">
        <template #default="{ row }">{{
          row.user_name ? `${row.user_name}(${row.username})` : '—'
        }}</template>
      </el-table-column>
      <el-table-column label="動作" width="110">
        <template #default="{ row }">
          <el-tag
            :type="
              ['login_failed', 'token_reuse', 'delete'].includes(row.action) ? 'danger' : 'info'
            "
          >
            {{ actionLabels[row.action] ?? row.action }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="對象" width="110">
        <template #default="{ row }">{{
          entityLabels[row.entity_type] ?? row.entity_type
        }}</template>
      </el-table-column>
      <el-table-column prop="summary" label="摘要" min-width="240" show-overflow-tooltip />
    </el-table>
    <div class="pager">
      <el-pagination
        v-model:current-page="query.page"
        v-model:page-size="query.size"
        :total="meta.total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        @current-change="load"
        @size-change="search"
      />
    </div>
  </div>
</template>

<style scoped>
.detail {
  padding: 8px 16px;
}
.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin: 6px 0 0;
}
</style>
