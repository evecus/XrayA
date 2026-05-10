<script setup>
import { ref } from 'vue'
import { useMainStore } from '../stores/main.js'
import * as api from '../api.js'
import Modal      from '../components/Modal.vue'
import ErrorModal from '../components/ErrorModal.vue'
import LogModal   from '../components/LogModal.vue'

const store = useMainStore()

// ── Import modal ──────────────────────────────────────────────────────────────
const showImport  = ref(false)
const importText  = ref('')
const importing   = ref(false)

async function doImport() {
  const links = importText.value.split('\n').map(s => s.trim()).filter(Boolean)
  if (!links.length) return
  importing.value = true
  try {
    const d = await api.importLinks(links)
    const msg = `已导入 ${d.added} 个节点` + (d.failed?.length ? `，${d.failed.length} 个失败` : '')
    store.toast(msg, 'success')
    showImport.value = false
    importText.value = ''
    await store.fetchNodes()
  } catch(e) {
    store.toast(e.message, 'error')
  } finally { importing.value = false }
}

// ── Connect / disconnect ──────────────────────────────────────────────────────
const errorTitle = ref('')
const errorMsg   = ref('')
const showError  = ref(false)
const showLogs   = ref(false)

async function connect(nodeId) {
  try {
    await api.connect(nodeId)
    store.toast('已连接', 'success')
    await store.fetchStatus()
  } catch {
    await store.fetchStatus()
    const msg = store.statusError || '连接失败'
    errorTitle.value = '连接失败'
    errorMsg.value   = msg
    showError.value  = true
  }
}

async function disconnect() {
  try {
    await api.disconnect()
    store.toast('已断开', 'info')
    await store.fetchStatus()
  } catch(e) { store.toast(e.message, 'error') }
}

async function deleteNode(id) {
  if (!confirm('确定删除该节点？')) return
  try {
    await api.deleteNode(id)
    store.nodes = store.nodes.filter(n => n.id !== id)
    if (id === store.activeNodeId) await store.fetchStatus()
  } catch(e) { store.toast(e.message, 'error') }
}

function openLogsFromError() {
  showError.value = false
  showLogs.value  = true
}
</script>

<template>
  <div>
    <div class="card">
      <div class="card-head">
        节点
        <span class="count-badge">{{ store.nodes.length }}</span>
        <div class="card-actions">
          <button class="btn btn-ghost btn-sm" @click="showImport = true">＋ 导入</button>
        </div>
      </div>

      <div class="card-body">
        <!-- Empty -->
        <div v-if="!store.nodes.length" class="empty">
          <div class="empty-icon">◈</div>
          <p>暂无节点。<br>请导入链接或添加订阅。</p>
        </div>

        <!-- Table -->
        <table v-else class="tbl">
          <thead>
            <tr>
              <th>名称</th>
              <th>地址</th>
              <th>协议</th>
              <th>分组</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="node in store.nodes"
              :key="node.id"
              :class="{ 'row-active': store.activeNodeId === node.id && store.isRunning }"
            >
              <td>
                <span class="truncate" style="display:block;max-width:220px;font-weight:500" :title="node.name">
                  {{ node.name }}
                </span>
              </td>
              <td>
                <span class="mono muted" style="font-size:12px">{{ node.address }}:{{ node.port }}</span>
              </td>
              <td>
                <span :class="['proto-tag', `proto-${node.protocol}`]">{{ node.protocol }}</span>
              </td>
              <td>
                <span v-if="node.groupId" class="group-tag">{{ store.subName(node.groupId) }}</span>
                <span v-else style="font-size:11px;color:var(--muted)">手动</span>
              </td>
              <td>
                <div class="row-actions">
                  <button
                    v-if="store.activeNodeId === node.id && store.isRunning"
                    class="btn btn-disconnect btn-sm"
                    @click="disconnect"
                  >⏹ 断开</button>
                  <button
                    v-else
                    class="btn btn-connect btn-sm"
                    @click="connect(node.id)"
                  >▶ 连接</button>
                  <button class="btn btn-danger btn-sm" @click="deleteNode(node.id)">✕</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Import modal -->
    <Modal v-if="showImport" title="导入节点" @close="showImport = false">
      <div class="field">
        <label>分享链接（每行一条）</label>
        <textarea
          class="input" v-model="importText" rows="9"
          placeholder="vmess://...&#10;vless://...&#10;trojan://...&#10;ss://...&#10;hysteria2://...&#10;hy2://..."
        ></textarea>
      </div>
      <template #foot>
        <button class="btn btn-ghost" @click="showImport = false">取消</button>
        <button class="btn btn-primary" :disabled="importing" @click="doImport">
          <span v-if="importing" class="spinner"></span>
          导入
        </button>
      </template>
    </Modal>

    <!-- Error modal -->
    <ErrorModal
      v-if="showError"
      :title="errorTitle"
      :message="errorMsg"
      @close="showError = false"
      @view-logs="openLogsFromError"
    />

    <!-- Log modal (opened from error dialog) -->
    <LogModal v-if="showLogs" @close="showLogs = false" />
  </div>
</template>
