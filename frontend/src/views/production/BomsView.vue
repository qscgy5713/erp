<script setup lang="ts">
// BOM(物料清單):每個成品一份,列出每 N 個成品需要的材料用量(基本單位);工單開單時依此展開領料明細
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { productionApi, type BomRow } from '@/api/production'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import ItemPicker from '@/components/ItemPicker.vue'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('production.bom.write'))
const { handle, fieldErrors, reset } = useApiError()

const query = reactive({ keyword: '', page: 1, size: 20 })
const rows = ref<BomRow[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await productionApi.boms({ ...query, keyword: query.keyword.trim() })
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

interface FormLine {
  item_id: number | null
  label?: string
  qty: string
  note: string
}

const dialog = ref(false)
const saving = ref(false)
const editing = ref<{ id: number; version: number } | null>(null)
const form = reactive({
  item_id: null as number | null,
  item_label: undefined as string | undefined,
  yield_qty: '1',
  is_active: true,
  note: '',
  lines: [] as FormLine[],
})

function openNew() {
  reset()
  editing.value = null
  Object.assign(form, {
    item_id: null,
    item_label: undefined,
    yield_qty: '1',
    is_active: true,
    note: '',
    lines: [{ item_id: null, qty: '', note: '' }],
  })
  dialog.value = true
}

async function openEdit(row: BomRow) {
  reset()
  try {
    const b = await productionApi.bom(row.id)
    editing.value = { id: b.id, version: b.version }
    Object.assign(form, {
      item_id: b.item_id,
      item_label: `${b.item_code} ${b.item_name}`,
      yield_qty: b.yield_qty,
      is_active: b.is_active,
      note: b.note,
      lines: b.lines.map((l) => ({
        item_id: l.item_id,
        label: `${l.item_code} ${l.item_name}`,
        qty: l.qty,
        note: l.note,
      })),
    })
    dialog.value = true
  } catch (e) {
    handle(e)
  }
}

async function save() {
  reset()
  saving.value = true
  const input = {
    item_id: form.item_id ?? 0,
    yield_qty: form.yield_qty,
    is_active: form.is_active,
    note: form.note,
    lines: form.lines
      .filter((l) => l.item_id !== null)
      .map((l) => ({
        item_id: l.item_id as number,
        qty: l.qty === '' ? '0' : l.qty,
        note: l.note,
      })),
    version: editing.value?.version,
  }
  try {
    if (editing.value) await productionApi.updateBom(editing.value.id, input)
    else await productionApi.createBom(input)
    ElMessage.success('已儲存')
    dialog.value = false
    await load()
  } catch (e) {
    handle(e)
  } finally {
    saving.value = false
  }
}

async function remove(row: BomRow) {
  try {
    await ElMessageBox.confirm(`刪除 ${row.item_code} ${row.item_name} 的 BOM?`, '刪除 BOM', {
      type: 'warning',
      confirmButtonText: '刪除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await productionApi.deleteBom(row.id)
    ElMessage.success('已刪除')
    await load()
  } catch (e) {
    handle(e)
  }
}

const lineError = (i: number) => fieldErrors.value[`lines.${i}`]

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-input
        v-model="query.keyword"
        placeholder="成品料號或品名"
        clearable
        style="width: 220px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button @click="search">查詢</el-button>
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="openNew">新增 BOM</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border>
      <el-table-column label="成品" min-width="240">
        <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
      </el-table-column>
      <el-table-column label="產出量" width="140" align="right">
        <template #default="{ row }">{{ row.yield_qty }} {{ row.unit_name }}</template>
      </el-table-column>
      <el-table-column prop="line_count" label="材料數" width="90" align="right" />
      <el-table-column label="狀態" width="90">
        <template #default="{ row }">
          <el-tag :type="row.is_active ? 'success' : 'info'">{{
            row.is_active ? '啟用' : '停用'
          }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="140">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">{{
            canWrite ? '修改' : '檢視'
          }}</el-button>
          <el-button v-if="canWrite" link type="danger" @click="remove(row)">刪除</el-button>
        </template>
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

    <el-dialog v-model="dialog" :title="editing ? '修改 BOM' : '新增 BOM'" width="760px">
      <el-form label-width="90px" :disabled="!canWrite">
        <el-row :gutter="12">
          <el-col :span="14">
            <el-form-item label="成品" :error="fieldErrors.item_id">
              <ItemPicker
                v-model="form.item_id"
                :label="form.item_label"
                item-type="goods"
                :disabled="!!editing"
              />
            </el-form-item>
          </el-col>
          <el-col :span="6">
            <el-form-item label="產出量" :error="fieldErrors.yield_qty">
              <el-input v-model="form.yield_qty" />
            </el-form-item>
          </el-col>
          <el-col :span="4">
            <el-form-item label-width="0"
              ><el-checkbox v-model="form.is_active">啟用</el-checkbox></el-form-item
            >
          </el-col>
        </el-row>
        <p class="hint">
          下面的材料用量是「產出 {{ form.yield_qty || '?' }} 個成品」需要的數量,一律以基本單位計。
        </p>
        <el-form-item label="備註"><el-input v-model="form.note" maxlength="2000" /></el-form-item>
        <div v-if="fieldErrors.lines" class="err">{{ fieldErrors.lines }}</div>
        <el-table :data="form.lines" border size="small">
          <el-table-column label="材料" min-width="260">
            <template #default="{ row, $index }">
              <ItemPicker
                v-model="row.item_id"
                :label="row.label"
                item-type="goods"
                :disabled="!canWrite"
              />
              <div v-if="lineError($index)" class="err">{{ lineError($index) }}</div>
            </template>
          </el-table-column>
          <el-table-column label="用量" width="130">
            <template #default="{ row }"><el-input v-model="row.qty" size="small" /></template>
          </el-table-column>
          <el-table-column label="備註" min-width="140">
            <template #default="{ row }"
              ><el-input v-model="row.note" size="small" maxlength="255"
            /></template>
          </el-table-column>
          <el-table-column v-if="canWrite" width="60">
            <template #default="{ $index }">
              <el-button link type="danger" @click="form.lines.splice($index, 1)">刪除</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-button
          v-if="canWrite"
          class="addline"
          @click="form.lines.push({ item_id: null, qty: '', note: '' })"
          >新增材料</el-button
        >
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">關閉</el-button>
        <el-button v-if="canWrite" type="primary" :loading="saving" @click="save">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin: 0 0 8px 90px;
}
.err {
  color: var(--el-color-danger);
  font-size: 12px;
}
.addline {
  margin-top: 8px;
}
</style>
