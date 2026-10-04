export type AIProviderId = 'anthropic' | 'openai' | 'gemini' | 'openrouter' | 'local' | 'custom'

export type AIProvider = {
  id: AIProviderId
  label: string
  defaultModel: string
  suggestedModels: string[]
  docsUrl: string
  needsBaseURL?: boolean
  /** A local model needs no key: Ollama and LM Studio accept anything. */
  keyOptional?: boolean
  defaultBaseURL?: string
}

/**
 * The built-in chat works with any of these, using the person's own key. The
 * other road — the local agent bridge — uses a subscription instead and needs
 * no key at all.
 */
export const AI_PROVIDERS: AIProvider[] = [
  {
    id: 'anthropic', label: 'Anthropic (Claude)',
    defaultModel: 'claude-sonnet-5-5',
    suggestedModels: ['claude-opus-5-5', 'claude-sonnet-5-5', 'claude-fable-5-1', 'claude-haiku-4-5-20251001'],
    docsUrl: 'https://console.anthropic.com/settings/keys',
  },
  {
    id: 'openai', label: 'OpenAI',
    defaultModel: 'gpt-5',
    suggestedModels: ['gpt-5', 'gpt-5-mini', 'gpt-4.1'],
    docsUrl: 'https://platform.openai.com/api-keys',
  },
  {
    id: 'gemini', label: 'Google (Gemini)',
    defaultModel: 'gemini-2.5-pro',
    suggestedModels: ['gemini-2.5-pro', 'gemini-2.5-flash'],
    docsUrl: 'https://aistudio.google.com/apikey',
  },
  {
    id: 'openrouter', label: 'OpenRouter (any model)',
    defaultModel: 'anthropic/claude-sonnet-4.5',
    suggestedModels: ['anthropic/claude-sonnet-4.5', 'openai/gpt-5', 'google/gemini-2.5-pro', 'x-ai/grok-4', 'deepseek/deepseek-chat'],
    docsUrl: 'https://openrouter.ai/keys',
  },
  {
    // A model running on your own machine costs nothing per request, like the
    // local agent: the second road without metered spending.
    id: 'local', label: 'Local model (Ollama, LM Studio, vLLM)',
    defaultModel: 'qwen2.5-coder:14b',
    suggestedModels: ['qwen2.5-coder:14b', 'qwen2.5-coder:32b', 'llama3.3:70b', 'devstral:24b'],
    docsUrl: 'https://ollama.com/download',
    needsBaseURL: true,
    keyOptional: true,
    defaultBaseURL: 'http://localhost:11434/v1',
  },
  {
    id: 'custom', label: 'Other, OpenAI-compatible',
    defaultModel: '',
    suggestedModels: [],
    docsUrl: '',
    needsBaseURL: true,
  },
]

// Keys live in this browser's localStorage and are sent with each chat request;
// the server never stores them. The storage key names predate the rename.
const KEY_STORE = 'sfa.ai.keys.v1'
type Store = Partial<Record<AIProviderId, { apiKey: string; model: string; baseURL?: string }>>

function readAll(): Store {
  try { return JSON.parse(localStorage.getItem(KEY_STORE) || '{}') } catch { return {} }
}
function writeAll(s: Store) { localStorage.setItem(KEY_STORE, JSON.stringify(s)) }

export function saveProviderKey(p: AIProviderId, apiKey: string, model: string, baseURL?: string) {
  const s = readAll(); s[p] = { apiKey, model, baseURL }; writeAll(s)
}
export function loadProviderKey(p: AIProviderId): { apiKey: string; model: string; baseURL?: string } | null {
  return readAll()[p] || null
}
export function forgetProviderKey(p: AIProviderId) {
  const s = readAll(); delete s[p]; writeAll(s)
}
export function hasProviderKey(p: AIProviderId): boolean { return !!readAll()[p] }

export function selectedProvider(): AIProviderId {
  return (localStorage.getItem('sfa.ai.selected.v1') as AIProviderId) || 'anthropic'
}
export function setSelectedProvider(p: AIProviderId) {
  localStorage.setItem('sfa.ai.selected.v1', p)
}

export function providerInfo(id: AIProviderId): AIProvider {
  return AI_PROVIDERS.find((p) => p.id === id) ?? AI_PROVIDERS[0]
}
