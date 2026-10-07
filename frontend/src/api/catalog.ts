// 映射与报价（S1-D 的接口清单：/api/supplier-offers、/api/offer-links）。⚠️ 待 D 落地对齐。
// 字段照总纲 §6：supplier_offers（货源商品）/ offer_links（按店映射，唯一键 store+offer+supplier）。
import { request } from './client';

export type Platform = '1688' | 'pdd' | 'taobao';
export type OrderChannel = 'self_use' | 'cross_border' | 'manual';

export interface SupplierOfferDTO {
  id: string;
  platform: Platform;
  item_id: string;
  sku_id: string;
  url: string;
  purchase_price: string;
  domestic_freight: string;
  stock: number;
  price_alert_threshold: string | null; // 空 = 默认阈值（上涨 ≥10% 告警，总纲 §13.1）
  order_channel: OrderChannel;
  followed: boolean; // 是否已关注（1688 库存/失效推送的前提，总纲 §5.10）
  status: string;
  created_at: string;
  updated_at: string;
}

export interface SupplierOfferReq {
  platform: Platform;
  item_id: string;
  sku_id: string;
  url: string;
  purchase_price: string;
  domestic_freight: string;
  stock: number;
  price_alert_threshold: string | null;
  order_channel: OrderChannel;
  followed: boolean;
}

export interface OfferLinkDTO {
  id: string;
  store_id: string;
  store_name: string;
  ozon_offer_id: string;
  supplier_offer_id: string;
  supplier_offer: Pick<SupplierOfferDTO, 'platform' | 'item_id' | 'sku_id' | 'purchase_price' | 'stock'> | null;
  priority: number; // 同一 offer 多货源时按 priority 排主备
  target_stock: number;
  last_pushed_stock: number | null;
}

export interface OfferLinkReq {
  store_id: string;
  ozon_offer_id: string;
  supplier_offer_id: string;
  priority: number;
  target_stock: number;
}

export interface Paged<T> {
  total: number;
  list: T[];
}

export function listSupplierOffers(
  q: { page: number; page_size: number; platform?: string; keyword?: string },
): Promise<Paged<SupplierOfferDTO>> {
  return request<Paged<SupplierOfferDTO>>({ method: 'GET', url: '/api/supplier-offers', params: q });
}

export function createSupplierOffer(body: SupplierOfferReq): Promise<SupplierOfferDTO> {
  return request<SupplierOfferDTO>({ method: 'POST', url: '/api/supplier-offers', data: body });
}

export function updateSupplierOffer(id: string, body: SupplierOfferReq): Promise<SupplierOfferDTO> {
  return request<SupplierOfferDTO>({ method: 'PUT', url: `/api/supplier-offers/${id}`, data: body });
}

export function deleteSupplierOffer(id: string): Promise<null> {
  return request<null>({ method: 'DELETE', url: `/api/supplier-offers/${id}` });
}

export function listOfferLinks(
  q: { page: number; page_size: number; store_id?: string; keyword?: string },
): Promise<Paged<OfferLinkDTO>> {
  return request<Paged<OfferLinkDTO>>({ method: 'GET', url: '/api/offer-links', params: q });
}

export function createOfferLink(body: OfferLinkReq): Promise<OfferLinkDTO> {
  return request<OfferLinkDTO>({ method: 'POST', url: '/api/offer-links', data: body });
}

export function updateOfferLink(id: string, body: OfferLinkReq): Promise<OfferLinkDTO> {
  return request<OfferLinkDTO>({ method: 'PUT', url: `/api/offer-links/${id}`, data: body });
}

export function deleteOfferLink(id: string): Promise<null> {
  return request<null>({ method: 'DELETE', url: `/api/offer-links/${id}` });
}
