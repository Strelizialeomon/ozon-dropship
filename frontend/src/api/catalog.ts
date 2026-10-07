// 映射与报价接口（后端已实现：backend/internal/catalog/handler.go、model.go）。
// ⚠️ 两个列表都是**不分页的裸数组**（后端如此），筛选参数见各自函数。
import { request } from './client';

export type Platform = '1688' | 'pdd' | 'taobao';
export type OrderChannel = 'self_use' | 'cross_border' | 'manual';
export type OfferStatus = 'active' | 'out_of_stock' | 'invalid';

export interface SupplierOfferDTO {
  id: string;
  platform: Platform;
  item_id: string;
  sku_id: string;
  url: string;
  purchase_price: string;
  domestic_freight: string;
  currency: string;
  stock: number;
  /** 比率（0–1，如 0.1 = 涨 10% 告警）；空 = 默认阈值。 */
  price_alert_threshold: string | null;
  order_channel: OrderChannel;
  followed: boolean;
  status: OfferStatus;
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
  currency: string;
  stock: number;
  price_alert_threshold: string | null;
  order_channel: OrderChannel;
  followed: boolean;
  status: OfferStatus;
}

export interface OfferLinkDTO {
  id: string;
  store_id: string;
  ozon_offer_id: string;
  supplier_offer_id: string;
  priority: number; // 数字小 = 主货源
  target_stock: number;
  last_pushed_stock: number | null;
  created_at: string;
  updated_at: string;
}

export interface OfferLinkReq {
  store_id: string;
  ozon_offer_id: string;
  supplier_offer_id: string;
  priority: number;
  target_stock: number;
}

/** GET /api/supplier-offers?platform=&status=&keyword= → 裸数组（不分页）。 */
export function listSupplierOffers(
  q: { platform?: string; status?: string; keyword?: string } = {},
): Promise<SupplierOfferDTO[]> {
  return request<SupplierOfferDTO[]>({ method: 'GET', url: '/api/supplier-offers', params: q });
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

/** GET /api/offer-links?store_id=&ozon_offer_id=&supplier_offer_id= → 裸数组（不分页）。 */
export function listOfferLinks(
  q: { store_id?: string; ozon_offer_id?: string; supplier_offer_id?: string } = {},
): Promise<OfferLinkDTO[]> {
  return request<OfferLinkDTO[]>({ method: 'GET', url: '/api/offer-links', params: q });
}

export function createOfferLink(body: OfferLinkReq): Promise<OfferLinkDTO> {
  return request<OfferLinkDTO>({ method: 'POST', url: '/api/offer-links', data: body });
}

/** PUT 只改 priority / target_stock（后端如此；store/offer 维度不可改，要改删了重建）。 */
export function updateOfferLink(id: string, body: { priority: number; target_stock: number }): Promise<OfferLinkDTO> {
  return request<OfferLinkDTO>({ method: 'PUT', url: `/api/offer-links/${id}`, data: body });
}

export function deleteOfferLink(id: string): Promise<null> {
  return request<null>({ method: 'DELETE', url: `/api/offer-links/${id}` });
}
