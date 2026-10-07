<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { masterdataApi, type Unit } from '@/api/masterdata'
import { useAuthStore } from '@/stores/auth'
import { useFormDialog } from '@/composables/useFormDialog'
import { required } from '@/utils/validators'
import ActiveTag from '@/components/ActiveTag.vue'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('masterdata.item.write'))
const rows = ref<Unit[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    rows.value = await masterdataApi.units()
  } catch (e) {
    dlg.handleError(e)
  } finally {
    loading.value = false
  }
}

const dlg = useFormDialog({
  defaults: () => ({ code: '', name: '', is_active: true }),
  fromRow: (r: Unit) => ({ code: r.code, name: r.name, is_active: r.is_active }),
  create: (f) => masterdataApi.unit.create(f),
  update: (id, f) => masterdataApi.unit.update(id, f),
  onSaved: load,
})
const { visible, saving, editing, formRef, form, fieldErrors } = dlg
onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="dlg.openCreate()">新增單位</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="code" label="代碼" width="140" />
      <el-table-column prop="name" label="名稱" />
      <el-table-column label="狀態" width="100">
        <template #default="{ row }"><ActiveTag :active="row.is_active" /></template>
      </el-table-column>
      <el-table-column v-if="canWrite" label="操作" width="90">
        <template #default="{ row }">
          <el-button link type="primary" @click="dlg.openEdit(row)">編輯</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="visible"
      :title="editing ? '編輯單位' : '新增單位'"
      width="420px"
      :close-on-click-modal="false"
    >
      <el-form ref="formRef" :model="form" label-width="70px" @submit.prevent="dlg.save">
        <el-form-item label="代碼" prop="code" :rules="required" :error="fieldErrors.code">
          <el-input v-model="form.code" maxlength="10" />
        </el-form-item>
        <el-form-item label="名稱" prop="name" :rules="required" :error="fieldErrors.name">
          <el-input v-model="form.name" maxlength="20" />
        </el-form-item>
        <el-form-item v-if="editing" label="啟用"
          ><el-switch v-model="form.is_active"
        /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="visible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="dlg.save">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
