<script setup lang="ts">
/**
 * 表情包选择器：点一下就发出去，右上角可以批量导入本地图片。
 *
 * 设计要点：
 *  - 列表来自 /api/stickers，导入时直接把 File 当请求体，2MB 以内、
 *    PNG/JPEG/GIF/WebP 有效（服务端还会再嗅探一次类型）。
 *  - 删除只对管理员开放（服务端 requireAdmin 校验），失败的提示原样透出。
 */
import { onMounted, ref } from 'vue';
import Icon from '@/components/Icon.vue';
import { deleteSticker, listStickers, stickerUrl, uploadSticker, type Sticker } from '@/api/stickers';
import { useUiStore } from '@/stores/ui';

const emit = defineEmits<{ pick: [id: string]; close: [] }>();
const ui = useUiStore();
const items = ref<Sticker[]>([]);
const loading = ref(false);
const uploading = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);

async function load() {
  loading.value = true;
  try {
    items.value = await listStickers();
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '表情包加载失败');
  } finally {
    loading.value = false;
  }
}

function openPicker() {
  fileInput.value?.click();
}

async function onFiles(ev: Event) {
  const input = ev.target as HTMLInputElement;
  const files = Array.from(input.files || []);
  input.value = '';
  if (!files.length) return;
  uploading.value = true;
  let ok = 0;
  for (const f of files) {
    try {
      await uploadSticker(f);
      ok += 1;
    } catch (e) {
      ui.error(`${f.name}：${e instanceof Error ? e.message : '导入失败'}`);
    }
  }
  uploading.value = false;
  if (ok) {
    ui.ok(`已导入 ${ok} 张表情`);
    await load();
  }
}

async function remove(item: Sticker, ev: Event) {
  ev.stopPropagation();
  if (!confirm(`删除表情「${item.name}」？`)) return;
  try {
    await deleteSticker(item.id);
    items.value = items.value.filter((it) => it.id !== item.id);
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '删除失败');
  }
}

onMounted(() => {
  void load();
});

defineExpose({ load });
</script>

<template>
  <div class="sticker-pop" role="dialog" aria-label="表情包">
    <div class="pop-head">
      <b>表情包</b>
      <div class="pop-actions">
        <button class="ghost" type="button" :disabled="uploading" @click="openPicker">
          <Icon name="plus" :size="13" />{{ uploading ? '导入中…' : '导入' }}
        </button>
        <button class="ghost" type="button" aria-label="关闭" @click="emit('close')">
          <Icon name="close" :size="13" />
        </button>
      </div>
    </div>
    <input
      ref="fileInput"
      class="hidden-input"
      type="file"
      accept="image/png,image/jpeg,image/gif,image/webp"
      multiple
      @change="onFiles"
    />
    <p v-if="loading" class="hint">加载中…</p>
    <p v-else-if="!items.length" class="hint">
      还没有表情，点「导入」选几张本地图片（PNG/JPG/GIF/WebP，单张 ≤ 2MB）
    </p>
    <div v-else class="grid">
      <button
        v-for="it in items"
        :key="it.id"
        class="cell"
        type="button"
        :title="it.name"
        @click="emit('pick', it.id)"
      >
        <img :src="stickerUrl(it.id)" :alt="it.name" loading="lazy" />
        <span
          v-if="!it.builtin"
          class="del"
          role="button"
          aria-label="删除（需要管理员）"
          @click="remove(it, $event)"
        >
          <Icon name="close" :size="10" />
        </span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.sticker-pop {
  position: absolute;
  right: 12px;
  bottom: 78px;
  width: 292px;
  max-height: 320px;
  display: flex;
  flex-direction: column;
  background: var(--card, #fff);
  border: 1px solid var(--line, #e6e3da);
  border-radius: 12px;
  box-shadow: 0 12px 32px rgba(40, 44, 34, 0.14);
  padding: 10px;
  z-index: 30;
}
.pop-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--line, #e6e3da);
}
.pop-head b {
  font-size: 12px;
  font-weight: 600;
  color: var(--ink-soft, #4d5648);
}
.pop-actions {
  display: flex;
  gap: 4px;
}
.ghost {
  height: 24px;
  padding: 0 7px;
  display: inline-flex;
  align-items: center;
  gap: 3px;
  border-radius: 7px;
  background: #eef1ea;
  color: #4d5648;
  font-size: 11px;
}
.ghost:disabled {
  opacity: 0.55;
}
.hidden-input {
  display: none;
}
.hint {
  font-size: 11px;
  line-height: 1.7;
  color: #7d8177;
  padding: 10px 4px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  /* 固定行高而不是 aspect-ratio：老浏览器/WebView 里 aspect-ratio + 百分比高度会失效，
     导致图片按原始尺寸撑破格子（表现为表情互相叠在一起）。 */
  grid-auto-rows: 64px;
  gap: 6px;
  padding: 8px 2px 2px;
  overflow-y: auto;
  overflow-x: hidden;
}
.cell {
  position: relative;
  height: 100%;
  min-height: 0;
  padding: 2px;
  border-radius: 8px;
  background: #f5f6f2;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}
.cell:hover {
  background: #eaf2e5;
}
.cell img {
  display: block;
  width: auto;
  height: auto;
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
}
.del {
  position: absolute;
  top: 2px;
  right: 2px;
  width: 16px;
  height: 16px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.45);
  color: #fff;
}
@media (max-width: 900px) {
  .sticker-pop {
    width: 260px;
    right: 8px;
    bottom: 72px;
  }
  .grid {
    grid-auto-rows: 56px;
  }
}
</style>
