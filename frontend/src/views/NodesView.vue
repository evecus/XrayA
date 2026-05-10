<script setup>
import { ref, computed } from 'vue'
import { useMainStore } from '../stores/main.js'
import * as api from '../api.js'
import Modal      from '../components/Modal.vue'
import ErrorModal from '../components/ErrorModal.vue'
import LogModal   from '../components/LogModal.vue'

const store = useMainStore()

// ── 当前显示的分组 ────────────────────────────────────────────────────────────
// null = 手动导入节点, sub.id = 对应订阅
const activeTab = ref(null)  // null 表示"导入的节点"

// 手动导入的节点（无 groupId）
const manualNodes = computed(() => store.nodes.filter(n => !n.groupId))

// 当前 tab 的订阅对象
const currentSub = computed(() => {
  if (activeTab.value === null) return null
  return store.subs.find(s => s.id === activeTab.value) ?? null
})

// 当前 tab 显示的节点
const displayedNodes = computed(() => {
  if (activeTab.value === null) return manualNodes.value
  return store.nodes.filter(n => n.groupId === activeTab.value)
})

// ── 导入弹窗（合并导入节点 + 订阅链接）────────────────────────────────────────
const showImport    = ref(false)
const importLinks   = ref('')
const subName       = ref('')
const subUrl        = ref('')
const importing     = ref(false)

async function doImport() {
  const links = importLinks.value.split('\n').map(s => s.trim()).filter(Boolean)
  const hasSub = subName.value.trim() && subUrl.value.trim()
  if (!links.length && !hasSub) {
    store.toast('请输入节点链接或订阅信息', 'error')
    return
  }
  importing.value = true
  try {
    // 先导入节点链接
    if (links.length) {
      const d = await api.importLinks(links)
      const msg = `已导入 ${d.added} 个节点` + (d.failed?.length ? `，${d.failed.length} 个失败` : '')
      store.toast(msg, 'success')
    }
    // 再添加并更新订阅
    if (hasSub) {
      await api.addSub({ name: subName.value.trim(), url: subUrl.value.trim() })
      store.toast('订阅已添加并正在获取...', 'success')
    }
    showImport.value = false
    importLinks.value = ''
    subName.value = ''
    subUrl.value = ''
    await Promise.all([store.fetchSubs(), store.fetchNodes()])
  } catch(e) {
    store.toast(e.message, 'error')
  } finally { importing.value = false }
}

// ── 编辑订阅弹窗 ──────────────────────────────────────────────────────────────
const showEditSub = ref(false)
const editSubName = ref('')
const editSubUrl  = ref('')
const editSaving  = ref(false)

function openEditSub() {
  if (!currentSub.value) return
  editSubName.value = currentSub.value.name
  editSubUrl.value  = currentSub.value.url
  showEditSub.value = true
}

async function doEditSub() {
  if (!editSubName.value.trim() || !editSubUrl.value.trim()) {
    store.toast('名称和 URL 不能为空', 'error')
    return
  }
  editSaving.value = true
  try {
    // 没有专门的编辑 API，先删再加
    await api.deleteSub(activeTab.value)
    await api.addSub({ name: editSubName.value.trim(), url: editSubUrl.value.trim() })
    store.toast('订阅已更新', 'success')
    showEditSub.value = false
    await Promise.all([store.fetchSubs(), store.fetchNodes()])
    // 重置 tab（因为 id 变了）
    const newSub = store.subs[store.subs.length - 1]
    if (newSub) activeTab.value = newSub.id
  } catch(e) { store.toast(e.message, 'error') }
  finally { editSaving.value = false }
}

// ── 更新订阅 ──────────────────────────────────────────────────────────────────
const updating = ref(false)
async function doUpdateSub() {
  if (!currentSub.value) return
  updating.value = true
  try {
    store.toast('正在更新...', 'info')
    await api.updateSub(currentSub.value.id)
    store.toast('更新成功', 'success')
    await Promise.all([store.fetchSubs(), store.fetchNodes()])
  } catch(e) { store.toast(e.message, 'error') }
  finally { updating.value = false }
}

// ── 删除订阅 ──────────────────────────────────────────────────────────────────
async function doDeleteSub() {
  if (!currentSub.value) return
  if (!confirm(`确定删除订阅「${currentSub.value.name}」及其所有节点？`)) return
  try {
    await api.deleteSub(currentSub.value.id)
    store.toast('订阅已删除', 'success')
    activeTab.value = null
    await Promise.all([store.fetchSubs(), store.fetchNodes()])
  } catch(e) { store.toast(e.message, 'error') }
}

// ── 删除节点 ──────────────────────────────────────────────────────────────────
async function deleteNode(id) {
  if (!confirm('确定删除该节点？')) return
  try {
    await api.deleteNode(id)
    store.nodes = store.nodes.filter(n => n.id !== id)
    if (id === store.activeNodeId) await store.fetchStatus()
    if (id === store.selectedNodeId) store.selectedNodeId = null
  } catch(e) { store.toast(e.message, 'error') }
}

// ── 选择节点作为代理节点 ──────────────────────────────────────────────────────
function selectNode(nodeId) {
  if (store.isRunning) return  // 运行中不能切换
  if (store.selectedNodeId === nodeId) {
    store.selectedNodeId = null
  } else {
    store.selectedNodeId = nodeId
  }
}

// ── 错误/日志弹窗 ─────────────────────────────────────────────────────────────
const errorTitle = ref('')
const errorMsg   = ref('')
const showError  = ref(false)
const showLogs   = ref(false)

function openLogsFromError() {
  showError.value = false
  showLogs.value  = true
}
</script>

<template>
  <div class="nodes-page">

    <!-- Tab 导航栏 -->
    <div class="tab-bar">
      <div class="tab-list">
        <!-- 导入的节点 tab -->
        <button
          :class="['tab-btn', activeTab === null ? 'active' : '']"
          @click="activeTab = null"
        >
          导入的节点
          <span class="tab-count">{{ manualNodes.length }}</span>
        </button>

        <!-- 订阅 tabs -->
        <button
          v-for="sub in store.subs"
          :key="sub.id"
          :class="['tab-btn', activeTab === sub.id ? 'active' : '']"
          @click="activeTab = sub.id"
        >
          {{ sub.name }}
          <span class="tab-count">{{ store.nodeCountOf(sub.id) }}</span>
        </button>
      </div>

      <div class="tab-actions">
        <!-- 订阅相关操作按钮（只在订阅 tab 显示） -->
        <template v-if="activeTab !== null">
          <button class="btn btn-ghost btn-sm" @click="openEditSub" title="编辑订阅">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
            编辑
          </button>
          <button class="btn btn-ghost btn-sm" :disabled="updating" @click="doUpdateSub" title="更新订阅">
            <span v-if="updating" class="spinner"></span>
            <svg v-else width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 11-2.12-9.36L23 10"/></svg>
            更新
          </button>
          <button class="btn btn-danger btn-sm" @click="doDeleteSub" title="删除订阅">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4a1 1 0 011-1h4a1 1 0 011 1v2"/></svg>
            删除
          </button>
        </template>

        <!-- 导入按钮（始终显示） -->
        <button class="btn btn-primary btn-sm" @click="showImport = true">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          导入
        </button>
      </div>
    </div>

    <!-- 节点列表 -->
    <div class="card nodes-card">
      <!-- 空状态 -->
      <div v-if="!displayedNodes.length" class="empty">
        <div class="empty-icon">◈</div>
        <p v-if="activeTab === null">暂无手动导入的节点。<br>点击右上角「导入」添加节点链接。</p>
        <p v-else>该订阅暂无节点。<br>点击「更新」获取订阅内容。</p>
      </div>

      <!-- 节点双列网格 -->
      <div v-else class="node-grid">
        <div
          v-for="node in displayedNodes"
          :key="node.id"
          :class="[
            'node-card',
            store.activeNodeId === node.id && store.isRunning ? 'node-card--active' : '',
            store.selectedNodeId === node.id && !store.isRunning ? 'node-card--selected' : ''
          ]"
        >
          <div class="node-card-left">
            <span v-if="store.selectedNodeId === node.id" class="selected-dot"></span>
            <div class="node-card-info">
              <span class="node-card-name" :title="node.name">{{ node.name }}</span>
              <span class="node-card-addr mono muted">{{ node.address }}:{{ node.port }}</span>
            </div>
          </div>
          <div class="node-card-right">
            <span :class="['proto-tag', `proto-${node.protocol?.toLowerCase()}`]">{{ node.protocol }}</span>
            <button
              :class="['btn btn-select btn-sm', store.selectedNodeId === node.id ? 'selected' : '']"
              :disabled="store.isRunning"
              @click="selectNode(node.id)"
              :title="store.isRunning ? '运行中无法切换节点' : '选择此节点'"
            >
              <svg v-if="store.selectedNodeId === node.id" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
              <svg v-else width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/></svg>
              {{ store.selectedNodeId === node.id ? '已选中' : '选择' }}
            </button>
            <button class="btn btn-danger btn-sm" @click="deleteNode(node.id)" title="删除节点">
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- 提示：运行时选中状态 -->
    <div v-if="store.isRunning && store.activeNode" class="running-hint">
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
      当前正在使用节点：<strong>{{ store.activeNode.name }}</strong>，停止后可重新选择节点。
    </div>

    <!-- 导入弹窗 -->
    <Modal v-if="showImport" title="导入节点 / 添加订阅" @close="showImport = false">
      <div class="field">
        <label>节点链接（每行一条，支持 vmess / vless / trojan / ss / hysteria2）</label>
        <textarea
          class="input" v-model="importLinks" rows="6"
          placeholder="vmess://...&#10;vless://...&#10;trojan://...&#10;ss://...&#10;hysteria2://..."
        ></textarea>
      </div>

      <div class="divider-label">— 或添加订阅 —</div>

      <div class="field">
        <label>订阅名称</label>
        <input class="input" v-model="subName" placeholder="我的订阅">
      </div>
      <div class="field">
        <label>订阅链接</label>
        <input class="input" v-model="subUrl" placeholder="https://...">
      </div>

      <template #foot>
        <button class="btn btn-ghost" @click="showImport = false">取消</button>
        <button class="btn btn-primary" :disabled="importing" @click="doImport">
          <span v-if="importing" class="spinner"></span>
          确认导入
        </button>
      </template>
    </Modal>

    <!-- 编辑订阅弹窗 -->
    <Modal v-if="showEditSub" title="编辑订阅" small @close="showEditSub = false">
      <div class="field">
        <label>订阅名称</label>
        <input class="input" v-model="editSubName" placeholder="订阅名称">
      </div>
      <div class="field">
        <label>订阅链接</label>
        <input class="input" v-model="editSubUrl" placeholder="https://...">
      </div>
      <template #foot>
        <button class="btn btn-ghost" @click="showEditSub = false">取消</button>
        <button class="btn btn-primary" :disabled="editSaving" @click="doEditSub">
          <span v-if="editSaving" class="spinner"></span>
          保存
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

    <LogModal v-if="showLogs" @close="showLogs = false" />
  </div>
</template>

<style scoped>
.nodes-page { display: flex; flex-direction: column; gap: 12px; width: 100%; }

/* Tab bar */
.tab-bar {
  display: flex; align-items: center; gap: 10px;
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 8px 12px;
  box-shadow: var(--shadow-sm); flex-wrap: wrap;
}
.tab-list { display: flex; gap: 4px; flex: 1; flex-wrap: wrap; }
.tab-btn {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 5px 12px; border-radius: 6px; border: 1px solid transparent;
  font-size: 13px; font-weight: 500; cursor: pointer; transition: all .14s;
  background: transparent; color: var(--muted2); white-space: nowrap;
}
.tab-btn:hover { background: var(--surface2); color: var(--text); }
.tab-btn.active { background: var(--accent-bg); color: var(--accent); border-color: var(--accent-glow); font-weight: 600; }
.tab-count {
  font-family: var(--mono); font-size: 10px;
  background: var(--surface2); border: 1px solid var(--border);
  padding: 1px 5px; border-radius: 10px; color: var(--muted); transition: all .14s;
}
.tab-btn.active .tab-count { background: var(--accent-bg); border-color: var(--accent-glow); color: var(--accent); }
.tab-actions { display: flex; gap: 6px; align-items: center; margin-left: auto; }

/* Nodes card wrapper */
.nodes-card { flex: 1; }

/* ── 双列节点网格 ── */
.node-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0;
}

/* 每张节点卡片 */
.node-card {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 14px; border-bottom: 1px solid var(--border);
  transition: background .12s; gap: 10px; min-width: 0;
}
/* 右列卡片加左边框作为分隔 */
.node-card:nth-child(even) { border-left: 1px solid var(--border); }
/* 最后两行去掉底边框 */
.node-card:nth-last-child(-n+2) { border-bottom: none; }
/* 如果总数为奇数，最后一个也去掉底边框 */
.node-card:last-child { border-bottom: none; }

.node-card:hover { background: var(--surface2); }
.node-card--selected { background: rgba(59,110,245,.04); }
.node-card--selected:hover { background: rgba(59,110,245,.08); }
.node-card--active { background: var(--green-bg); }
.node-card--active:hover { background: rgba(22,163,74,.12); }

.node-card-left {
  display: flex; align-items: center; gap: 8px;
  min-width: 0; flex: 1; overflow: hidden;
}
.node-card-info { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.node-card-name {
  font-size: 13px; font-weight: 500; color: var(--text);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.node-card-addr {
  font-size: 11px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}

.node-card-right {
  display: flex; align-items: center; gap: 6px; flex-shrink: 0;
}

.selected-dot {
  width: 7px; height: 7px; border-radius: 50%;
  background: var(--accent); flex-shrink: 0;
  box-shadow: 0 0 0 2px var(--accent-bg);
}

/* Running hint */
.running-hint {
  display: flex; align-items: center; gap: 7px;
  padding: 9px 14px; border-radius: 8px;
  background: var(--green-bg); border: 1px solid var(--green-bd);
  color: var(--green); font-size: 12px;
}
.running-hint strong { font-weight: 600; }

/* Import modal divider */
.divider-label {
  text-align: center; font-size: 11px; color: var(--muted);
  margin: 4px 0 14px; letter-spacing: .04em;
}

/* 小屏降为单列 */
@media (max-width: 700px) {
  .node-grid { grid-template-columns: 1fr; }
  .node-card:nth-child(even) { border-left: none; }
  .node-card:nth-last-child(-n+2) { border-bottom: 1px solid var(--border); }
  .node-card:last-child { border-bottom: none; }
}
</style>
