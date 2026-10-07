import { request } from './http'

export interface Health {
  status: string
}

export const getHealth = () => request<Health>('/health')
