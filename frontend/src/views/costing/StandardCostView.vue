<script setup lang="ts">
// 標準成本:成品的目標單位成本(基本單位),只做管理用的差異分析,不入帳
import { computed, onMounted, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { costingApi, type StandardCost, type StandardCostVersion } from '@/api/costing'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const canWrite = computed(() => auth.can(['costing.standard.write']))
const { handle } = useApiError()

const state = reactive({
  keyword: '',
  onlyBom: true,
  page: 1,
  size: 20,
  rows: [] as StandardCost[],
  meta: { page: 1, size: 20, total: 0 } as PageMeta,
  loading: false,
})
const edits = reactive<Record<number, string>>({})

async function load() {
  state.loading = true
  try {
    const res = await costingApi.standardCosts({
      keyword: state.keyword.trim(),
      only_bom: state.onlyBom,
      page: state.page,
      size: state.size,
    })
    state.rows = res.items
    state.meta = res.meta
    for (const r of res.items) edits[r.item_id] = r.standard_cost ?? ''
  } catch (e) {
    handle(e)
  } finally {
    state.loading = false
  }
}

async function save(row: StandardCost) {
  const v = (edits[row.item_id] ?? '').trim()
  if (v === (row.standard_cost ?? '')) return
  try {
    await costingApi.setStandardCost(row.item_id, v === '' ? null : v)
    ElMessage.success(v === '' ? '已清除' : '已儲存')
    await load()
  } catch (e) {
    handle(e)
    edits[row.item_id] = row.standard_cost ?? ''
  }
}

// ---- 版本:不同生效日的標準成本,差異報表依各筆完工日取當時有效的一筆 ----
const BASE = '2000-01-01'
const vDialog = reactive({
  open: false,
  row: null as StandardCost | null,
  rows: [] as StandardCostVersion[],
  date: '',
  cost: '',
})

async function openVersions(row: StandardCost) {
  vDialog.row = row
  vDialog.date = ''
  vDialog.cost = ''
  try {
    vDialog.rows = await costingApi.standardVersions(row.item_id)
    vDialog.open = true
  } catch (e) {
    handle(e)
  }
}

async function addVersion() {
  if (!vDialog.row || !vDialog.date || vDialog.cost.trim() === '') return
  try {
    await costingApi.setStandardVersion(vDialog.row.item_id, vDialog.cost.trim(), vDialog.date)
    vDialog.rows = await costingApi.standardVersions(vDialog.row.item_id)
    vDialog.date = ''
    vDialog.cost = ''
    ElMessage.success('已儲存')
    await load()
  } catch (e) {
    handle(e)
  }
}

async function removeVersion(v: StandardCostVersion) {
  if (!vDialog.row) return
  try {
    await costingApi.setStandardVersion(vDialog.row.item_id, null, v.effective_from)
    vDialog.rows = await costingApi.standardVersions(vDialog.row.item_id)
    ElMessage.success('已刪除')
    await load()
  } catch (e) {
    handle(e)
  }
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-input
        v-model="state.keyword"
        placeholder="料號 / 品名"
        clearable
        style="width: 220px"
        @keyup.enter="((state.page = 1), load())"
        @clear="((state.page = 1), load())"
      />
      <el-checkbox v-model="state.onlyBom" @change="((state.page = 1), load())"
        >只看有 BOM 的成品</el-checkbox
      >
      <span class="hint"
        >單位成本以基本單位計;清空欄位即清除全部版本。上方輸入的是「一直有效」的版本,要依日期調整請按「版本」。標準成本不入帳。</span
      >
    </div>
    <el-table v-loading="state.loading" :data="state.rows" border empty-text="沒有資料">
      <el-table-column prop="item_code" label="料號" width="140" />
      <el-table-column prop="item_name" label="品名" min-width="200" />
      <el-table-column prop="unit_name" label="單位" width="80" />
      <el-table-column label="標準單位成本" width="200">
        <template #default="{ row }">
          <el-input
            v-model="edits[row.item_id]"
            size="small"
            :disabled="!canWrite"
            placeholder="未設定"
            @blur="save(row)"
            @keyup.enter="save(row)"
          />
        </template>
      </el-table-column>
      <el-table-column label="版本" width="100">
        <template #default="{ row }">
          <el-button link type="primary" @click="openVersions(row)">版本</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-dialog
      v-model="vDialog.open"
      :title="`標準成本版本 ${vDialog.row?.item_code ?? ''}`"
      width="560px"
    >
      <p class="hint">
        完工日落在哪個生效日之後,就用那一版;「一直有效」是最早的基準。標準成本不入帳。
      </p>
      <el-table :data="vDialog.rows" border size="small" empty-text="還沒有版本">
        <el-table-column label="生效日" width="140">
          <template #default="{ row }">{{
            row.effective_from === BASE ? '一直有效' : row.effective_from
          }}</template>
        </el-table-column>
        <el-table-column prop="standard_cost" label="標準單位成本" align="right" />
        <el-table-column prop="updated_by_name" label="維護者" width="110" />
        <el-table-column v-if="canWrite" width="70">
          <template #default="{ row }">
            <el-button link type="danger" @click="removeVersion(row)">刪除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="canWrite" class="add-version">
        <el-date-picker
          v-model="vDialog.date"
          value-format="YYYY-MM-DD"
          placeholder="生效日"
          style="width: 160px"
        />
        <el-input v-model="vDialog.cost" placeholder="標準單位成本" style="width: 160px" />
        <el-button type="primary" @click="addVersion">新增 / 更新版本</el-button>
      </div>
    </el-dialog>
    <div class="pager">
      <el-pagination
        v-model:current-page="state.page"
        v-model:page-size="state.size"
        :total="state.meta.total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        @current-change="load"
        @size-change="((state.page = 1), load())"
      />
    </div>
  </div>
</template>

<style scoped>
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.add-version {
  display: flex;
  gap: 8px;
  margin-top: 12px;
  flex-wrap: wrap;
}
</style>
