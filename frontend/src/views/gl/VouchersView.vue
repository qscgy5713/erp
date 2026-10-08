<script setup lang="ts">
// 傳票列表:手動傳票與業務單據自動拋轉的傳票
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  glApi,
  sourceLabels,
  type AccountOption,
  type VoucherRow,
  type VoucherStatus,
} from '@/api/gl'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = computed(() => auth.can(['gl.voucher.write']))
const { handle } = useApiError()

const statusLabels: Record<VoucherStatus, { label: string; type: 'info' | 'success' | 'danger' }> =
  {
    draft: { label: '草稿', type: 'info' },
    posted: { label: '已過帳', type: 'success' },
    voided: { label: '已作廢', type: 'danger' },
  }

const query = reactive({
  status: ((route.query.status as string | undefined) ?? '') as VoucherStatus | '',
  source: '' as '' | 'manual' | 'auto',
  account_id: null as number | null,
  keyword: '',
  range: null as [string, string] | null,
  page: 1,
  size: 20,
})
const rows = ref<VoucherRow[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const accounts = ref<AccountOption[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { range, ...rest } = query
    const res = await glApi.vouchers({
      ...rest,
      keyword: rest.keyword.trim(),
      from: range?.[0],
      to: range?.[1],
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

const open = (row: VoucherRow) => router.push({ name: 'gl-voucher', params: { id: row.id } })
const money = (v: string) => Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 0 })

onMounted(async () => {
  load()
  try {
    accounts.value = await glApi.accountOptions()
  } catch (e) {
    handle(e)
  }
})
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-select
        v-model="query.status"
        clearable
        placeholder="狀態"
        style="width: 110px"
        @change="search"
      >
        <el-option v-for="(s, v) in statusLabels" :key="v" :label="s.label" :value="v" />
      </el-select>
      <el-select
        v-model="query.source"
        clearable
        placeholder="來源"
        style="width: 110px"
        @change="search"
      >
        <el-option label="手動" value="manual" />
        <el-option label="自動拋轉" value="auto" />
      </el-select>
      <el-select
        v-model="query.account_id"
        clearable
        filterable
        placeholder="科目"
        style="width: 180px"
        @change="search"
      >
        <el-option v-for="a in accounts" :key="a.id" :label="`${a.code} ${a.name}`" :value="a.id" />
      </el-select>
      <el-date-picker
        v-model="query.range"
        type="daterange"
        value-format="YYYY-MM-DD"
        start-placeholder="開始日期"
        end-placeholder="結束日期"
        style="width: 240px"
        @change="search"
      />
      <el-input
        v-model="query.keyword"
        placeholder="傳票號 / 來源單號 / 摘要"
        clearable
        style="width: 200px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button @click="search">查詢</el-button>
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="router.push({ name: 'gl-voucher-new' })">
        新增傳票
      </el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border highlight-current-row @row-dblclick="open">
      <el-table-column label="傳票號" width="160">
        <template #default="{ row }">
          <el-button link type="primary" @click="open(row)">{{ row.doc_no }}</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="voucher_date" label="日期" width="110" />
      <el-table-column label="來源" width="190">
        <template #default="{ row }">
          {{ sourceLabels[row.source_type] ?? row.source_type }}
          <span v-if="row.source_no" class="hint">{{ row.source_no }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="摘要" min-width="200" show-overflow-tooltip />
      <el-table-column label="金額" width="120" align="right">
        <template #default="{ row }">{{ money(row.total_amount) }}</template>
      </el-table-column>
      <el-table-column label="狀態" width="170">
        <template #default="{ row }">
          <el-tag :type="statusLabels[row.status as VoucherStatus].type">
            {{ statusLabels[row.status as VoucherStatus].label }}
          </el-tag>
          <el-tag v-if="row.reversal_of" type="warning" effect="plain" class="ml">沖銷傳票</el-tag>
          <el-tag v-else-if="row.reversed" type="warning" effect="plain" class="ml">已沖銷</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_by_name" label="建立者" width="110" />
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
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-left: 6px;
}
.ml {
  margin-left: 6px;
}
</style>
