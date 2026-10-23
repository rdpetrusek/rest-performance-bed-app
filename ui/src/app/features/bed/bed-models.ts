// Wire shapes the Go server answers with. Only the fields the dashboard
// actually reads are declared - see docs/protocol.md in the repo root for
// the bed's full payloads.

export interface DeviceInfo {
  name?: string;
  model?: string;
  configuration?: string;
  address?: string;
}

export interface Preferences {
  mode?: string;
  firmness?: number;
}

export interface BodyData {
  present?: boolean;
  heartrate?: number;
  moving?: boolean;
  breathing?: { rate?: number };
}

export interface TemperatureData {
  cpu?: number;
}

export interface AirData {
  filling?: boolean;
  targets?: number[];
  pressures?: number[];
  flows?: string[];
  minimum?: number;
  maximum?: number;
}

/** The sensor pad's pressure map; rows/columns are 0 while it's disconnected. */
export interface SurfaceData {
  rows?: number;
  columns?: number;
}

export interface LiveSnapshot {
  device?: DeviceInfo;
  preferences?: Preferences;
  body?: BodyData;
  temperature?: TemperatureData;
  air?: AirData;
  surface?: SurfaceData;
}

/**
 * Each configuration the firmware knows, mapped to its twin with the sensor-pad
 * requirement flipped ("z00" = never reads the pad). Mirrors bed.PadBypass in Go.
 */
export const PAD_BYPASS: Readonly<Record<string, string>> = {
  publ5z65ha: 'publ5z00ha',
  publ5z00ha: 'publ5z65ha',
  pubr5z65ha: 'pubr5z00ha',
  pubr5z00ha: 'pubr5z65ha',
  pibl3z49ha: 'pibl3z00ha',
  pibl3z00ha: 'pibl3z49ha',
  pibr3z49ha: 'pibr3z00ha',
  pibr3z00ha: 'pibr3z49ha',
};

/** Pressure limits every bed seen so far reports, until a live air event arrives. */
export const DEFAULT_AIR_LIMITS = { min: 5, max: 50 } as const;

export interface HistoryRow<T> {
  ts: string;
  data: T;
}

export interface Stats {
  min: number;
  max: number;
  avg: number;
}

export interface SessionSummary {
  found: boolean;
  reason?: string;
  ongoing: boolean;
  start?: string;
  end?: string;
  duration_minutes: number;
  sample_count: number;
  movement_events: number;
  movement_ratio: number;
  heart_rate: Stats | null;
  respiration_rate: Stats | null;
}

export type ManualProfile = readonly [number, number, number, number];

/** One bed found by the server's periodic network scan (GET /api/devices). */
export interface DiscoveredDevice {
  id: string;
  name: string;
  address: string;
  class: string;
  model?: string;
  configuration?: string;
}
