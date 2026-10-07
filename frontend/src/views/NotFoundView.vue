<template>
  <el-result icon="warning" title="404" sub-title="找不到頁面">
    <template #extra>
      <el-button type="primary" @click="$router.push('/')">回首頁</el-button>
    </template>
  </el-result>
</template>
