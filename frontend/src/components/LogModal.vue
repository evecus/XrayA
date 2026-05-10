<script setup>
import { ref, nextTick, onMounted } from 'vue'
import * as api from '../api.js'
import Modal from './Modal.vue'

const emit = defineEmits(['close'])

const logs    = ref([])
const logEl   = ref(null)
const loading = ref(false)

async function fetchLogs() {
  loading.value = true
  try {
    const d = await api.getLogs()
    logs.value = d.logs ?? []
    nextTick(() => { if (logEl.value) logEl.value.scrollTop = logEl.value.scrollHeight })
  } finally { loading.value = false }
}

function logClass(line) {
  if (/error|ERR/i.test(line))  return 'log-err'
  if (/warn|WARN/i.test(line))  return 'log-warn'
  return ''
}

onMounted(fetchLogs)
</script>

<template>
  <Modal title="xray 日志" large @close="emit('close')">
    <template #title>
      <span>xray 日志</span>
      <button class="btn btn-ghost btn-sm" style="margin-left:10px" @click="fetchLogs" :disabled="loading">
        <span v-if="loading" class="spinner" style="width:11px;height:11px"></span>
        <span v-else>↻</span>
        刷新
      </button>
    </template>

    <div class="log-view" ref="logEl">
      <div v-if="!logs.length" style="color:var(--muted)">暂无日志。</div>
      <div v-for="(line, i) in logs" :key="i" :class="logClass(line)">{{ line }}</div>
    </div>
  </Modal>
</template>
