<template>
  <el-result icon="error" title="403" sub-title="沒有權限檢視此頁面">
    <template #extra>
      <el-button type="primary" @click="$router.push('/')">回首頁</el-button>
    </template>
  </el-result>
</template>
