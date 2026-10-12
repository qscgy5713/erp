<script setup lang="ts">
// MRP 計畫參數:前置時間(採購件 = 採購前置,有 BOM 的 = 生產前置)、最小訂購 / 生產量、批量倍數;全部為 0 = 不限制
import { computed, onMounted, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { productionApi, type MrpParam } from '@/api/production'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const canWrite = computed(() => auth.can(['production.bom.write']))
const { handle } = useApiError()

const state = reactive({
  keyword: '',
  page: 1,
  size: 20,
  rows: [] as MrpParam[],
  meta: { page: 1, size: 20, total: 0 } as PageMeta,
  loading: false,
})
interface Edit {
  lead: string
  min: string
  multiple: string
}
const edits = reactive<Record<number, Edit>>({})

async function load() {
  state.loading = true
  try {
    const res = await productionApi.mrpParams({
      keyword: state.keyword.trim(),
      page: state.page,
      size: state.size,
    })
    state.rows = res.items
    state.meta = res.meta
    for (const r of res.items) {
      edits[r.item_id] = {
        lead: String(r.lead_time_days),
        min: r.min_order_qty,
        multiple: r.order_multiple,
      }
    }
  } catch (e) {
    handle(e)
  } finally {
    state.loading = false
  }
}

async function save(row: MrpParam) {
  const e = edits[row.item_id]
  if (!e) return
  const lead = Number(e.lead || '0')
  if (
    lead === row.lead_time_days &&
    Number(e.min || 0) === Number(row.min_order_qty) &&
    Number(e.multiple || 0) === Number(row.order_multiple)
  ) {
    return
  }
  try {
    await productionApi.setMrpParams(row.item_id, {
      lead_time_days: lead,
      min_order_qty: e.min || '0',
      order_multiple: e.multiple || '0',
    })
    ElMessage.success('已儲存')
    await load()
  } catch (err) {
    handle(err)
    await load()
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
      <span class="hint"
        >數量以基本單位計。建議補貨量先補到最小量,再進位到批量的整數倍;全部填 0 = 不限制。</span
      >
    </div>
    <el-table v-loading="state.loading" :data="state.rows" border empty-text="沒有資料">
      <el-table-column prop="item_code" label="料號" width="140" />
      <el-table-column prop="item_name" label="品名" min-width="200" />
      <el-table-column prop="unit_name" label="單位" width="80" />
      <el-table-column label="類型" width="80">
        <template #default="{ row }">{{ row.has_bom ? '自製' : '採購' }}</template>
      </el-table-column>
      <el-table-column label="前置時間(天)" width="140">
        <template #default="{ row }">
          <el-input
            v-if="edits[row.item_id]"
            v-model="edits[row.item_id]!.lead"
            size="small"
            :disabled="!canWrite"
            @blur="save(row)"
            @keyup.enter="save(row)"
          />
        </template>
      </el-table-column>
      <el-table-column label="最小訂購 / 生產量" width="160">
        <template #default="{ row }">
          <el-input
            v-if="edits[row.item_id]"
            v-model="edits[row.item_id]!.min"
            size="small"
            :disabled="!canWrite"
            @blur="save(row)"
            @keyup.enter="save(row)"
          />
        </template>
      </el-table-column>
      <el-table-column label="批量倍數" width="140">
        <template #default="{ row }">
          <el-input
            v-if="edits[row.item_id]"
            v-model="edits[row.item_id]!.multiple"
            size="small"
            :disabled="!canWrite"
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
