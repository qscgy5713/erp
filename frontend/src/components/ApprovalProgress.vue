<script setup lang="ts">
// 多層簽核進度:只有套用了簽核規則(兩層以上)的單據才顯示;載入後把進度回報給父元件,讓它決定能否顯示「核准」
import { computed, ref, watch } from 'vue'
import { approvalApi, type ApprovalProgress, type ProgressStep } from '@/api/approval'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  docType: string
  docId: number | null | undefined
  /** 單據狀態與版本:任一改變就重新載入 */
  status: string | undefined
  version: number | undefined
}>()
const emit = defineEmits<{ loaded: [progress: ApprovalProgress | null] }>()

const progress = ref<ApprovalProgress | null>(null)
let seq = 0

async function load() {
  const mine = ++seq
  if (!props.docId || !props.status || props.status === 'draft' || props.status === 'voided') {
    progress.value = null
    emit('loaded', null)
    return
  }
  try {
    const p = await approvalApi.progress(props.docType, props.docId)
    if (mine !== seq) return
    progress.value = p.required > 1 ? p : null
    emit('loaded', progress.value)
  } catch {
    if (mine !== seq) return
    progress.value = null // 進度只是輔助資訊,載入失敗不打擾主要流程
    emit('loaded', null)
  }
}

// 單層核准或第 1 層核准後,單據版本不一定變,所以也要依狀態與目前層級重新載入
/** 依層級分組:或簽層任一格核准即通過,會簽層全部格核准才通過 */
const layers = computed(() => {
  const out: { no: number; title: string; slots: ProgressStep[]; done: boolean }[] = []
  for (const s of progress.value?.steps ?? []) {
    let l = out.find((x) => x.no === s.step)
    if (!l) {
      l = { no: s.step, title: '', slots: [], done: false }
      out.push(l)
    }
    l.slots.push(s)
  }
  for (const l of out) {
    const all = l.slots[0]?.mode === 'all'
    l.done = all ? l.slots.every((s) => s.done) : l.slots.some((s) => s.done)
    const names = l.slots.map((s) => s.role_name)
    l.title =
      names.length > 1
        ? `第 ${l.no} 層 ${all ? '會簽' : '或簽'}${all ? ` ${l.slots.filter((s) => s.done).length}/${l.slots.length}` : ''}`
        : `第 ${l.no} 層 ${names[0]}`
  }
  return out
})
const doneLayers = computed(() => layers.value.filter((l) => l.done).length)

watch(() => [props.docId, props.status, props.version], load, { immediate: true })
defineExpose({ reload: load })
</script>

<template>
  <el-card v-if="progress" shadow="never" class="approval">
    <div class="head">簽核流程</div>
    <el-steps :active="doneLayers" finish-status="success" align-center>
      <el-step v-for="l in layers" :key="l.no" :title="l.title">
        <template #description>
          <div v-for="s in l.slots" :key="s.slot" :class="{ waived: s.waived }">
            <template v-if="l.slots.length > 1">{{ s.role_name }}:</template>
            <template v-if="s.done"
              >{{ s.approver_name }}{{ s.acting_for ? `(代 ${s.acting_for})` : '' }}
              {{ formatDateTime(s.approved_at) }}</template
            >
            <template v-else-if="s.waived">免簽</template>
            <template v-else>待核准</template>
          </div>
        </template>
      </el-step>
    </el-steps>
  </el-card>
</template>

<style scoped>
.approval {
  margin-bottom: 12px;
}
.waived {
  color: var(--el-text-color-placeholder);
}
.head {
  font-weight: 600;
  margin-bottom: 12px;
}
</style>
