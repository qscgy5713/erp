<script setup lang="ts">
// 通知設定:每個人決定要不要收各類 Email 通知(沒設定過的預設為開);寄信位址是帳號資料裡的 email
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { authApi, type NotificationPref } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const { handle } = useApiError()
const prefs = ref<NotificationPref[]>([])
const loading = ref(false)
const testing = ref(false)

async function load() {
  loading.value = true
  try {
    prefs.value = await authApi.notificationPrefs()
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

async function toggle(p: NotificationPref, on: boolean) {
  try {
    prefs.value = await authApi.updateNotificationPrefs({ [p.event]: on })
    ElMessage.success(on ? `已開啟「${p.label}」` : `已關閉「${p.label}」`)
  } catch (e) {
    handle(e)
    await load() // 失敗時還原畫面
  }
}

async function sendTest() {
  testing.value = true
  try {
    const r = await authApi.testNotification()
    ElMessage.success(`測試信已排入,約 30 秒內寄到 ${r.to}`)
  } catch (e) {
    handle(e)
  } finally {
    testing.value = false
  }
}

onMounted(load)
</script>

<template>
  <el-card style="max-width: 720px" v-loading="loading">
    <template #header>Email 通知</template>
    <p class="hint">
      通知寄到帳號資料中的 email{{
        auth.user?.email ? `(${auth.user.email})` : '(尚未設定,請洽管理員)'
      }}。 關閉後該類通知不會再寄給你;是否收得到仍取決於你的權限。
    </p>
    <div v-for="p in prefs" :key="p.event" class="row">
      <div>
        <div class="label">{{ p.label }}</div>
        <div class="desc">{{ p.desc }}</div>
      </div>
      <el-switch
        :model-value="p.enabled"
        @change="(v: string | number | boolean) => toggle(p, v === true)"
      />
    </div>
    <el-button class="test" :loading="testing" @click="sendTest">寄一封測試信給我</el-button>
  </el-card>
</template>

<style scoped>
.hint {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0 0 12px;
}
.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.label {
  font-weight: 600;
}
.desc {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-top: 2px;
}
.test {
  margin-top: 16px;
}
</style>
