// Template library API client (user-level template management)
import client from './client'

export interface Template {
  id: number
  name: string
  content?: string // Only present in GET /api/templates/{name}
  is_default: boolean
  created_at: string
  updated_at: string
  ref_count?: number // Only present in list response
}

export interface TemplateListResponse {
  templates: Template[]
}

export interface CreateTemplateRequest {
  name: string
  content: string
}

export interface UpdateTemplateRequest {
  content: string
}

export interface DeleteTemplateResponse {
  success: boolean
  ref_count: number
}

// List all templates in user's library
export function listTemplates(): Promise<TemplateListResponse> {
  return client.get<unknown, TemplateListResponse>('/templates')
}

// Get a template by name (includes content)
export function getTemplate(name: string): Promise<Template> {
  return client.get<unknown, Template>(`/templates/${encodeURIComponent(name)}`)
}

// Create a new template
export function createTemplate(req: CreateTemplateRequest): Promise<Template> {
  return client.post<unknown, Template>('/templates', req)
}

// Update template content
export function updateTemplate(
  name: string,
  req: UpdateTemplateRequest
): Promise<{ success: boolean }> {
  return client.put<unknown, { success: boolean }>(`/templates/${encodeURIComponent(name)}`, req)
}

// Delete a template (returns reference count)
export function deleteTemplate(name: string): Promise<DeleteTemplateResponse> {
  return client.delete<unknown, DeleteTemplateResponse>(`/templates/${encodeURIComponent(name)}`)
}

// Set a template as default
export function setDefaultTemplate(name: string): Promise<{ success: boolean }> {
  return client.put<unknown, { success: boolean }>(`/templates/${encodeURIComponent(name)}/default`)
}

// Reset a template to embedded default
export function resetTemplate(name: string): Promise<{ message: string }> {
  return client.post<unknown, { message: string }>(`/templates/${encodeURIComponent(name)}/reset`)
}

// Template version history
export interface TemplateVersion {
  version: number
  created_at: string
}

export interface TemplateVersionListResponse {
  versions: TemplateVersion[]
}

export interface TemplateVersionContent {
  version: number
  content: string
  created_at: string
}

// List all versions of a template
export function listTemplateVersions(name: string): Promise<TemplateVersionListResponse> {
  return client.get<unknown, TemplateVersionListResponse>(
    `/templates/${encodeURIComponent(name)}/versions`
  )
}

// Get specific version content
export function getTemplateVersion(name: string, version: number): Promise<TemplateVersionContent> {
  return client.get<unknown, TemplateVersionContent>(
    `/templates/${encodeURIComponent(name)}/versions/${version}`
  )
}
