<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { productionApi, type ScStatus, type SubcontractRow } from '@/api/production'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { scStatusLabels, scStatusTypes } from '@/utils/subcontract'

const router = useRouter()
const auth = useAuthStore()
const canWrite = computed(() => auth.can('production.subcontract.write'))
const { handle } = useApiError()

const query = reactive({ status: '' as ScStatus | '', keyword: '', page: 1, size: 20 })
const rows = ref<SubcontractRow[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await productionApi.subcontracts({ ...query, keyword: query.keyword.trim() })
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

const open = (row: SubcontractRow) => router.push({ name: 'subcontract', params: { id: row.id } })

onMounted(load)
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
        <el-option v-for="(label, v) in scStatusLabels" :key="v" :label="label" :value="v" />
      </el-select>
      <el-input
        v-model="query.keyword"
        placeholder="單號、加工廠、成品"
        clearable
        style="width: 200px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button @click="search">查詢</el-button>
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="router.push({ name: 'subcontract-new' })"
        >新增委外加工單</el-button
      >
    </div>

    <el-table v-loading="loading" :data="rows" border highlight-current-row @row-dblclick="open">
      <el-table-column label="單號" width="160">
        <template #default="{ row }"
          ><el-button link type="primary" @click="open(row)">{{ row.doc_no }}</el-button></template
        >
      </el-table-column>
      <el-table-column prop="doc_date" label="日期" width="110" />
      <el-table-column label="加工廠" min-width="160">
        <template #default="{ row }">{{ row.supplier_code }} {{ row.supplier_name }}</template>
      </el-table-column>
      <el-table-column label="成品" min-width="200">
        <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
      </el-table-column>
      <el-table-column label="收回 / 預計" width="150" align="right">
        <template #default="{ row }"
          >{{ Number(row.received_qty) > 0 ? `${row.received_qty} / ` : '' }}{{ row.plan_qty }}
          {{ row.unit_name }}</template
        >
      </el-table-column>
      <el-table-column label="加工費單價" width="110" align="right">
        <template #default="{ row }">{{ Number(row.unit_fee).toLocaleString('zh-TW') }}</template>
      </el-table-column>
      <el-table-column prop="due_date" label="預定收回" width="110" />
      <el-table-column label="狀態" width="90">
        <template #default="{ row }"
          ><el-tag :type="scStatusTypes[row.status as ScStatus]">{{
            scStatusLabels[row.status as ScStatus]
          }}</el-tag></template
        >
      </el-table-column>
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
