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

export interface UpdateEndpointGeoConfigRequest {
  geo_mode: string // 'off' | 'observe' | 'enforce'
  geo_countries: string // Comma-separated country codes (e.g., "CN,US")
  geo_provinces: string // Comma-separated province codes/names
}

// Update endpoint geo allowlist configuration (pull-guard ticket 07/08)
export function updateEndpointGeoConfig(
  id: number,
  geoMode: string,
  geoCountries: string,
  geoProvinces: string
): Promise<{ success: boolean }> {
  return client.put<unknown, { success: boolean }>(`/endpoints/${id}/geo-config`, {
    geo_mode: geoMode,
    geo_countries: geoCountries,
    geo_provinces: geoProvinces
  })
}
