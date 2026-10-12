<script setup lang="ts">
// 標準成本:成品的目標單位成本(基本單位),只做管理用的差異分析,不入帳
import { computed, onMounted, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { costingApi, type StandardCost } from '@/api/costing'
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
      <span class="hint">單位成本以基本單位計;清空欄位即清除。標準成本不入帳。</span>
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
    </el-table>
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
