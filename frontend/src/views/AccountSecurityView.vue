<script setup lang="ts">
// 帳號安全:雙因素驗證(TOTP)。以驗證器 App(Google Authenticator、Microsoft Authenticator、1Password…)掃描 QR Code,
// 登入時除了密碼再輸入 App 顯示的 6 位數;另有一組一次性的備援碼,手機不在身邊時使用。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import QRCode from 'qrcode'
import { ElMessage, ElMessageBox } from 'element-plus'
import { authApi, type TwoFactorSetup, type TwoFactorStatus } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const router = useRouter()
const { handle } = useApiError()

const status = ref<TwoFactorStatus | null>(null)
const loading = ref(false)

// 設定流程
const setup = ref<TwoFactorSetup | null>(null)
const qr = ref('')
const code = ref('')
const working = ref(false)
// 備援碼只顯示一次
const recovery = ref<string[] | null>(null)

const mustSetup = computed(() => auth.mustSetup2FA)

async function load() {
  loading.value = true
  try {
    status.value = await authApi.twoFactorStatus()
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

async function startSetup() {
  working.value = true
  try {
    setup.value = await authApi.twoFactorSetup()
    qr.value = await QRCode.toDataURL(setup.value.otpauth_uri, { width: 200, margin: 1 })
    code.value = ''
  } catch (e) {
    handle(e)
  } finally {
    working.value = false
  }
}

async function enable() {
  if (!code.value.trim()) return
  working.value = true
  try {
    recovery.value = (await authApi.twoFactorEnable(code.value.trim())).recovery_codes
    setup.value = null
    code.value = ''
    ElMessage.success('已啟用雙因素驗證')
    await Promise.all([load(), auth.reloadMe()])
  } catch (e) {
    handle(e)
  } finally {
    working.value = false
  }
}

/** 停用 / 重新產生備援碼:再輸入一次密碼與目前的驗證碼 */
async function confirmWithCredentials(
  title: string,
  message: string,
): Promise<[string, string] | null> {
  try {
    const pw = await ElMessageBox.prompt(message, title, {
      inputType: 'password',
      inputPlaceholder: '目前密碼',
      confirmButtonText: '下一步',
      cancelButtonText: '取消',
    })
    const cd = await ElMessageBox.prompt(
      '請輸入驗證器 App 目前的 6 位數驗證碼(或一組備援碼)',
      title,
      {
        inputPlaceholder: '123456',
        confirmButtonText: '確定',
        cancelButtonText: '取消',
      },
    )
    return [pw.value, cd.value]
  } catch {
    return null
  }
}

async function disable() {
  const cred = await confirmWithCredentials(
    '停用雙因素驗證',
    '停用後登入只需要密碼,安全性會降低。請輸入密碼確認。',
  )
  if (!cred) return
  try {
    await authApi.twoFactorDisable(...cred)
    ElMessage.success('已停用雙因素驗證')
    await Promise.all([load(), auth.reloadMe()])
  } catch (e) {
    handle(e)
  }
}

async function regenerate() {
  const cred = await confirmWithCredentials(
    '重新產生備援碼',
    '舊的備援碼會全部作廢。請輸入密碼確認。',
  )
  if (!cred) return
  try {
    recovery.value = (await authApi.recoveryCodes(...cred)).recovery_codes
    await load()
  } catch (e) {
    handle(e)
  }
}

function copyCodes() {
  void navigator.clipboard
    ?.writeText((recovery.value ?? []).join('\n'))
    .then(() => ElMessage.success('已複製'))
}

function finish() {
  recovery.value = null
  if (
    mustSetup.value === false &&
    router.currentRoute.value.name === 'account-security' &&
    history.length > 1
  ) {
    // 因公司政策被導來的人,設定完成後回首頁
    router.replace('/')
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading" style="max-width: 640px">
    <el-alert
      v-if="mustSetup"
      type="warning"
      :closable="false"
      show-icon
      class="mb"
      title="公司要求所有使用者使用雙因素驗證,請先完成下面的設定才能使用系統。"
    />

    <el-card shadow="never" class="mb">
      <template #header>雙因素驗證(TOTP)</template>

      <template v-if="status && !status.enabled && !setup">
        <p>
          尚未啟用。啟用後,登入除了密碼,還要輸入驗證器 App 顯示的 6 位數,即使密碼外洩也無法登入。
        </p>
        <el-button type="primary" :loading="working" @click="startSetup">開始設定</el-button>
      </template>

      <template v-if="setup">
        <ol class="steps">
          <li>
            在手機安裝驗證器 App(Google Authenticator、Microsoft Authenticator、1Password 等)。
          </li>
          <li>用 App 掃描下面的 QR Code;不能掃描時,改為手動輸入密鑰。</li>
          <li>輸入 App 顯示的 6 位數驗證碼,完成啟用。</li>
        </ol>
        <div class="qr">
          <img :src="qr" alt="QR Code" width="200" height="200" />
          <div>
            <div class="muted">手動輸入密鑰(時間型、6 位數、30 秒)</div>
            <code class="secret">{{ setup.secret }}</code>
          </div>
        </div>
        <el-form inline @submit.prevent="enable">
          <el-form-item>
            <el-input
              v-model="code"
              placeholder="6 位數驗證碼"
              maxlength="6"
              style="width: 160px"
              autofocus
            />
          </el-form-item>
          <el-button type="primary" native-type="submit" :loading="working" @click="enable"
            >啟用</el-button
          >
          <el-button @click="setup = null">取消</el-button>
        </el-form>
      </template>

      <template v-if="status?.enabled && !setup">
        <el-tag type="success" class="mr">已啟用</el-tag>
        <span class="muted">剩餘備援碼 {{ status.recovery_codes_left }} 組</span>
        <div class="actions">
          <el-button @click="regenerate">重新產生備援碼</el-button>
          <el-button v-if="!status.company_requires" type="danger" plain @click="disable">
            停用雙因素驗證
          </el-button>
          <span v-else class="muted">公司要求雙因素驗證,不能停用</span>
        </div>
      </template>
    </el-card>

    <el-dialog
      :model-value="!!recovery"
      title="請保存備援碼"
      width="440px"
      :close-on-click-modal="false"
      :show-close="false"
    >
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="這些備援碼只會顯示這一次。每組只能用一次;手機遺失時用它登入,然後重新設定。請存放在安全的地方(例如密碼管理器),不要和密碼放在一起。"
        class="mb"
      />
      <div class="codes">
        <code v-for="c in recovery ?? []" :key="c">{{ c }}</code>
      </div>
      <template #footer>
        <el-button @click="copyCodes">複製</el-button>
        <el-button type="primary" @click="finish">我已保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.mb {
  margin-bottom: 16px;
}
.mr {
  margin-right: 8px;
}
.muted {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.steps {
  padding-left: 20px;
  line-height: 1.8;
}
.qr {
  display: flex;
  gap: 20px;
  align-items: center;
  margin: 12px 0 16px;
  flex-wrap: wrap;
}
.secret {
  display: block;
  margin-top: 6px;
  font-size: 15px;
  letter-spacing: 1px;
  word-break: break-all;
  user-select: all;
}
.actions {
  margin-top: 16px;
  display: flex;
  gap: 12px;
  align-items: center;
}
.codes {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px 16px;
  font-size: 15px;
}
.codes code {
  user-select: all;
}
</style>
