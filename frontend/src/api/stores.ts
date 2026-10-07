// 店铺（S1-A 已实现，形状照 backend/internal/store/store.go 的 Shop / shopReq）。
import { request } from './client';

export interface StoreDTO {
  id: string;
  name: string;
  mode: 'rfbs' | 'fbp' | 'local';
  client_id: string;
  currency: string;
  default_relay_point_id: string | null;
  push_enabled: boolean;
  ship_early: boolean;
  last_sync_at: string | null;
  status: 'active' | 'paused';
  created_at: string;
  updated_at: string;
}

export interface StoreReq {
  name: string;
  mode: 'rfbs' | 'fbp' | 'local';
  client_id: string;
  currency: string;
  default_relay_point_id: string | null;
  push_enabled: boolean;
  ship_early: boolean;
  status?: 'active' | 'paused';
}

export function listStores(): Promise<StoreDTO[]> {
  return request<StoreDTO[]>({ method: 'GET', url: '/api/stores' });
}

export function createStore(body: StoreReq): Promise<StoreDTO> {
  return request<StoreDTO>({ method: 'POST', url: '/api/stores', data: body });
}

export function updateStore(id: string, body: StoreReq): Promise<StoreDTO> {
  return request<StoreDTO>({ method: 'PUT', url: `/api/stores/${id}`, data: body });
}

export function deleteStore(id: string): Promise<null> {
  return request<null>({ method: 'DELETE', url: `/api/stores/${id}` });
}
