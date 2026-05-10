<script setup>
import { ref } from 'vue'
import { useMainStore } from '../stores/main.js'
import * as api from '../api.js'
import Modal from '../components/Modal.vue'

const store = useMainStore()

// ── Add subscription modal ────────────────────────────────────────────────────
const showAdd  = ref(false)
const newName  = ref('')
const newUrl   = ref('')
const adding   = ref(false)

async function doAdd() {
  if (!newName.value.trim() || !newUrl.value.trim()) {
    store.toast('名称和 URL 不能为空', 'error')
    return
  }
  adding.value = true
  try {
    await api.addSub({ name: newName.value.trim(), url: newUrl.value.trim() })
    store.toast('订阅已添加，正在获取...', 'success')
    showAdd.value = false
    newName.value = ''
    newUrl.value  = ''
    await Promise.all([store.fetchSubs(), store.fetchNodes()])
  } catch(e) {
    store.toast(e.message, 'error')
  } finally { adding.value = false }
}

// ── Update / delete ───────────────────────────────────────────────────────────
const updating = ref(null)

async function doUpdate(id) {
  updating.value = id
  try {
    store.toast('正在更新...', 'info')
    await api.updateSub(id)
    store.toast('更新成功', 'success')
    await Promise.all([store.fetchSubs(), store.fetchNodes()])
  } catch(e) {
    store.toast(e.message, 'error')
  } finally { updating.value = null }
}

async function doDelete(id) {
  if (!confirm('确定删除该订阅及其所有节点？')) return
  try {
    await api.deleteSub(id)
    await Promise.all([store.fetchSubs(), store.fetchNodes()])
  } catch(e) { store.toast(e.message, 'error') }
}

function formatDate(v) {
  if (!v) return '从未更新'
  return new Date(v).toLocaleString('zh-CN')
}
</script>

<template>
  <div>
    <div class="card">
      <div class="card-head">
        订阅
        <span class="count-badge">{{ store.subs.length }}</span>
        <div class="card-actions">
          <button class="btn btn-ghost btn-sm" @click="showAdd = true">＋ 添加</button>
        </div>
      </div>

      <div class="card-body">
        <div v-if="!store.subs.length" class="empty">
          <div class="empty-icon">◈</div>
          <p>暂无订阅。<br>点击右上角添加。</p>
        </div>

        <table v-else class="tbl">
          <thead>
            <tr>
              <th>名称 / URL</th>
              <th>节点 / 更新时间</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="sub in store.subs" :key="sub.id">
              <td style="max-width:0">
                <div style="font-weight:500;margin-bottom:2px">{{ sub.name }}</div>
                <div class="mono muted truncate" style="font-size:11px;max-width:340px">{{ sub.url }}</div>
              </td>
              <td style="white-space:nowrap">
                <div style="font-size:12px">{{ store.nodeCountOf(sub.id) }} 个节点</div>
                <div class="muted" style="font-size:11px;margin-top:1px">{{ formatDate(sub.updated) }}</div>
              </td>
              <td>
                <div style="display:flex;gap:5px">
                  <button
                    class="btn btn-ghost btn-sm"
                    :disabled="updating === sub.id"
                    @click="doUpdate(sub.id)"
                  >
                    <span v-if="updating === sub.id" class="spinner"></span>
                    <span v-else>↻</span>
                    更新
                  </button>
                  <button class="btn btn-danger btn-sm" @click="doDelete(sub.id)">✕</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Add subscription modal -->
    <Modal v-if="showAdd" title="添加订阅" @close="showAdd = false">
      <div class="field">
        <label>名称</label>
        <input class="input" v-model="newName" placeholder="我的订阅">
      </div>
      <div class="field">
        <label>URL</label>
        <input class="input" v-model="newUrl" placeholder="https://..." @keydown.enter="doAdd">
      </div>
      <template #foot>
        <button class="btn btn-ghost" @click="showAdd = false">取消</button>
        <button class="btn btn-primary" :disabled="adding" @click="doAdd">
          <span v-if="adding" class="spinner"></span>
          添加并获取
        </button>
      </template>
    </Modal>
  </div>
</template>
