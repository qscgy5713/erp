<script setup lang="ts">
// 多層簽核進度:只有套用了簽核規則(兩層以上)的單據才顯示;載入後把進度回報給父元件,讓它決定能否顯示「核准」
import { ref, watch } from 'vue'
import { approvalApi, type ApprovalProgress } from '@/api/approval'
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
watch(() => [props.docId, props.status, props.version], load, { immediate: true })
defineExpose({ reload: load })
</script>

<template>
  <el-card v-if="progress" shadow="never" class="approval">
    <div class="head">簽核流程</div>
    <el-steps
      :active="progress.steps.filter((s) => s.done).length"
      finish-status="success"
      align-center
    >
      <el-step
        v-for="s in progress.steps"
        :key="s.step"
        :title="`第 ${s.step} 層 ${s.role_name}`"
        :description="s.done ? `${s.approver_name} ${formatDateTime(s.approved_at)}` : '待核准'"
      />
    </el-steps>
  </el-card>
</template>

<style scoped>
.approval {
  margin-bottom: 12px;
}
.head {
  font-weight: 600;
  margin-bottom: 12px;
}
</style>
