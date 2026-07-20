<template>
  <el-select
    v-loading="loading"
    :model-value="modelValue"
    multiple
    filterable
    placeholder="选择上游节点"
    style="width: 100%"
    @update:model-value="handleChange"
  >
    <el-option-group label="机场节点">
      <el-option
        v-for="node in airportNodes"
        :key="node.key"
        :label="`${node.name} (${node.region})`"
        :value="node.key"
        :disabled="!node.available"
      >
        <div style="display: flex; justify-content: space-between; align-items: center">
          <span>{{ node.name }}</span>
          <div>
            <el-tag v-if="node.available" type="success" size="small">可用</el-tag>
            <el-tag v-else type="info" size="small">不可用</el-tag>
            <el-text type="info" size="small" style="margin-left: 8px">
              {{ node.region }}
            </el-text>
          </div>
        </div>
      </el-option>
    </el-option-group>

    <el-option-group label="自建节点">
      <el-option
        v-for="node in selfNodes"
        :key="node.key"
        :label="`${node.name} (${node.region})`"
        :value="node.key"
        :disabled="!node.available"
      >
        <div style="display: flex; justify-content: space-between; align-items: center">
          <span>{{ node.name }}</span>
          <div>
            <el-tag v-if="node.available" type="success" size="small">可用</el-tag>
            <el-tag v-else type="info" size="small">不可用</el-tag>
            <el-text type="info" size="small" style="margin-left: 8px">
              {{ node.region }}
            </el-text>
          </div>
        </div>
      </el-option>
    </el-option-group>
  </el-select>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'

interface Node {
  key: string
  name: string
  region: string
  available: boolean
  source: string
}

const props = defineProps<{
  modelValue: string[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
}>()

const nodes = ref<Node[]>([])
const loading = ref(false)

const airportNodes = computed(() => nodes.value.filter((n) => n.source !== 'self'))

const selfNodes = computed(() => nodes.value.filter((n) => n.source === 'self'))

const handleChange = (value: string[]) => {
  emit('update:modelValue', value)
}

const loadNodes = async () => {
  loading.value = true
  try {
    const data = await client.get<any, Node[]>('/nodes')
    nodes.value = data
  } catch (error) {
    ElMessage.error('加载节点列表失败')
  } finally {
    loading.value = false
  }
}

onMounted(loadNodes)
</script>
