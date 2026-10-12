<script setup lang="ts">
// 售價表維護:客戶別 / 一般價、數量階梯、有效期間(單價為未稅、基本單位)
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { salesApi, type SalesPrice } from '@/api/sales'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import ItemPicker from '@/components/ItemPicker.vue'
import PartnerPicker from '@/components/PartnerPicker.vue'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('sales.price.write'))
const rows = ref<SalesPrice[]>([])
const loading = ref(false)
const { handle, fieldErrors, autoClear } = useApiError()

const filter = reactive({ item_id: null as number | null, customer_id: null as number | null })

async function load() {
  loading.value = true
  try {
    rows.value = await salesApi.prices({
      item_id: filter.item_id ?? undefined,
      customer_id: filter.customer_id ?? undefined,
    })
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

const visible = ref(false)
const saving = ref(false)
const blank = () => ({
  customer_id: null as number | null,
  item_id: null as number | null,
  currency: 'TWD',
  min_qty: '0',
  unit_price: '',
  valid_from: null as string | null,
  valid_to: null as string | null,
  note: '',
})
const form = reactive(blank())
autoClear(form)

function openCreate() {
  Object.assign(form, blank())
  fieldErrors.value = {}
  visible.value = true
}

async function save() {
  saving.value = true
  try {
    await salesApi.createPrice({
      ...form,
      valid_from: form.valid_from || null,
      valid_to: form.valid_to || null,
    })
    ElMessage.success('已新增')
    visible.value = false
    await load()
  } catch (e) {
    handle(e)
  } finally {
    saving.value = false
  }
}

async function remove(row: SalesPrice) {
  try {
    await ElMessageBox.confirm(`刪除「${row.item_code}」這筆售價?`, '刪除', { type: 'warning' })
  } catch {
    return
  }
  try {
    await salesApi.deletePrice(row.id)
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
      <ItemPicker v-model="filter.item_id" style="width: 240px" @update:model-value="load" />
      <PartnerPicker
        v-model="filter.customer_id"
        kind="customer"
        clearable
        style="width: 220px"
        @update:model-value="load"
      />
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="openCreate">新增售價</el-button>
    </div>
    <p class="hint">
      開單選料品時,依序採用:客戶專屬價 → 一般價(同層取數量門檻最高者)→ 料品建議售價。
    </p>
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column label="料品" min-width="200">
        <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
      </el-table-column>
      <el-table-column label="客戶" min-width="140">
        <template #default="{ row }">{{ row.customer_name ?? '(所有客戶)' }}</template>
      </el-table-column>
      <el-table-column prop="currency" label="幣別" width="70" />
      <el-table-column label="數量 ≥" width="100" align="right">
        <template #default="{ row }">{{ Number(row.min_qty) }}</template>
      </el-table-column>
      <el-table-column label="單價(基本單位)" width="130" align="right">
        <template #default="{ row }">{{ Number(row.unit_price) }}</template>
      </el-table-column>
      <el-table-column label="有效期間" width="210">
        <template #default="{ row }">
          {{ row.valid_from ?? '不限' }} ~ {{ row.valid_to ?? '不限' }}
        </template>
      </el-table-column>
      <el-table-column prop="note" label="備註" min-width="120" />
      <el-table-column v-if="canWrite" label="操作" width="80">
        <template #default="{ row }">
          <el-button link type="danger" @click="remove(row)">刪除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="visible" title="新增售價" width="520px" :close-on-click-modal="false">
      <el-form :model="form" label-width="100px" @submit.prevent="save">
        <el-form-item label="料品" required :error="fieldErrors.item_id">
          <ItemPicker v-model="form.item_id" />
        </el-form-item>
        <el-form-item label="客戶" :error="fieldErrors.customer_id">
          <PartnerPicker v-model="form.customer_id" kind="customer" clearable />
          <div class="hint">留空 = 適用所有客戶</div>
        </el-form-item>
        <el-form-item label="幣別" :error="fieldErrors.currency">
          <el-input v-model="form.currency" maxlength="3" style="width: 100px" />
        </el-form-item>
        <el-form-item label="數量 ≥" :error="fieldErrors.min_qty">
          <el-input v-model="form.min_qty" style="width: 140px" />
        </el-form-item>
        <el-form-item label="單價" required :error="fieldErrors.unit_price">
          <el-input v-model="form.unit_price" style="width: 140px" />
        </el-form-item>
        <el-form-item label="起日" :error="fieldErrors.valid_from">
          <el-date-picker v-model="form.valid_from" type="date" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item label="迄日" :error="fieldErrors.valid_to">
          <el-date-picker v-model="form.valid_to" type="date" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item label="備註"><el-input v-model="form.note" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="visible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin: 4px 0 10px;
}
</style>
