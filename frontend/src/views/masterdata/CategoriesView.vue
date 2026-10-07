<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { masterdataApi, type ItemCategory } from '@/api/masterdata'
import { useAuthStore } from '@/stores/auth'
import { useFormDialog } from '@/composables/useFormDialog'
import { buildTree, descendantIds } from '@/utils/tree'
import { required } from '@/utils/validators'
import ActiveTag from '@/components/ActiveTag.vue'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('masterdata.item.write'))
const list = ref<ItemCategory[]>([])
const loading = ref(false)
const tree = computed(() => buildTree(list.value))

async function load() {
  loading.value = true
  try {
    list.value = await masterdataApi.categories()
  } catch (e) {
    dlg.handleError(e)
  } finally {
    loading.value = false
  }
}

const dlg = useFormDialog({
  defaults: () => ({
    parent_id: null as number | null,
    code: '',
    name: '',
    sort_order: 0,
    is_active: true,
  }),
  fromRow: (r: ItemCategory) => ({
    parent_id: r.parent_id,
    code: r.code,
    name: r.name,
    sort_order: r.sort_order,
    is_active: r.is_active,
  }),
  create: (f) => masterdataApi.category.create(f),
  update: (id, f) => masterdataApi.category.update(id, f),
  onSaved: load,
})
const { visible, saving, editing, formRef, form, fieldErrors } = dlg

// 編輯時排除自己與下層,避免形成循環
const parentOptions = computed(() => {
  const excluded = editing.value ? descendantIds(list.value, editing.value.id) : new Set<number>()
  return buildTree(list.value.filter((c) => !excluded.has(c.id)))
})
onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="dlg.openCreate()">新增分類</el-button>
    </div>
    <el-table v-loading="loading" :data="tree" row-key="id" default-expand-all border>
      <el-table-column prop="code" label="代碼" min-width="140" />
      <el-table-column prop="name" label="名稱" min-width="160" />
      <el-table-column prop="item_count" label="料品數" width="90" align="right" />
      <el-table-column prop="sort_order" label="排序" width="80" align="right" />
      <el-table-column label="狀態" width="90">
        <template #default="{ row }"><ActiveTag :active="row.is_active" /></template>
      </el-table-column>
      <el-table-column v-if="canWrite" label="操作" width="180">
        <template #default="{ row }">
          <el-button link type="primary" @click="dlg.openEdit(row)">編輯</el-button>
          <el-button link type="primary" @click="dlg.openCreate({ parent_id: row.id })"
            >新增下層</el-button
          >
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="visible"
      :title="editing ? '編輯分類' : '新增分類'"
      width="460px"
      :close-on-click-modal="false"
    >
      <el-form ref="formRef" :model="form" label-width="90px" @submit.prevent="dlg.save">
        <el-form-item label="上層分類" :error="fieldErrors.parent_id">
          <el-tree-select
            v-model="form.parent_id"
            :data="parentOptions"
            :props="{ label: 'name', children: 'children' }"
            node-key="id"
            check-strictly
            clearable
            placeholder="(無,為最上層)"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="代碼" prop="code" :rules="required" :error="fieldErrors.code">
          <el-input v-model="form.code" maxlength="20" />
        </el-form-item>
        <el-form-item label="名稱" prop="name" :rules="required" :error="fieldErrors.name">
          <el-input v-model="form.name" maxlength="100" />
        </el-form-item>
        <el-form-item label="排序"
          ><el-input-number v-model="form.sort_order" :min="0"
        /></el-form-item>
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
