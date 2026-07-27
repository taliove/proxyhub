// Endpoint (subscription address) API client
import client from './client'

export interface UpdateEndpointTemplateRequest {
  template_name: string // Empty string to unbind (follow default)
}

// Update endpoint template binding
export function updateEndpointTemplate(
  id: number,
  templateName: string
): Promise<{ success: boolean }> {
  return client.put<unknown, { success: boolean }>(`/endpoints/${id}/template`, {
    template_name: templateName
  })
}
