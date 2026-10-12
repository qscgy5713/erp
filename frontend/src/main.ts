import { createApp } from 'vue'
import { createPinia } from 'pinia'
// 以程式呼叫的元件(ElMessage / ElMessageBox)不會被自動引入樣式,這裡補上
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'

import App from './App.vue'
import router from './router'
import { installAuthHooks } from './stores/auth'

const app = createApp(App)

app.use(createPinia())
installAuthHooks(() => {
  const current = router.currentRoute.value
  if (!current.meta.public) {
    router.replace({ name: 'login', query: { redirect: current.fullPath } })
  }
})
app.use(router)
app.mount('#app')
