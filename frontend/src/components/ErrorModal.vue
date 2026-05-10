<script setup>
import Modal from './Modal.vue'
defineProps({ title: String, message: String })
const emit = defineEmits(['close', 'view-logs'])
</script>

<template>
  <Modal :title="title || '错误'" @close="emit('close')">
    <template #title>
      <span style="color:var(--red)">⚠ {{ title || '错误' }}</span>
    </template>

    <pre class="error-pre">{{ message }}</pre>
    <div class="hint">
      💡 <b>xray exited unexpectedly</b> 最常见原因是 <b>geoip.dat / geosite.dat 缺失</b>。<br>
      请将这两个文件放到 xraya 的 <code>data/</code> 目录下，再重试。<br>
      其他原因：① xray 二进制不存在或无执行权限 &nbsp;② 节点配置解析失败
    </div>

    <template #foot>
      <button class="btn btn-ghost" @click="emit('view-logs')">查看日志</button>
      <button class="btn btn-primary" @click="emit('close')">关闭</button>
    </template>
  </Modal>
</template>

<style scoped>
.error-pre {
  font-family: var(--mono); font-size: 11px; white-space: pre-wrap;
  word-break: break-all; color: var(--text); background: var(--bg);
  border: 1px solid var(--border); border-radius: 8px;
  padding: 12px; margin: 0; max-height: 260px; overflow-y: auto; line-height: 1.7;
}
.hint {
  font-size: 12px; color: var(--muted2); margin-top: 14px; line-height: 1.8;
}
.hint b { color: var(--text); }
.hint code {
  background: var(--surface2); padding: 1px 5px;
  border-radius: 3px; font-family: var(--mono);
}
</style>
