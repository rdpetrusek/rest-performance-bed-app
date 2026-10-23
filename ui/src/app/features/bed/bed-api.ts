import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import {
  BodyData,
  DiscoveredDevice,
  HistoryRow,
  LiveSnapshot,
  ManualProfile,
  Preferences,
  SessionSummary,
} from './bed-models';

/** Wraps the Go server's JSON API. Holds no state - see bed-store.ts for that. */
@Injectable({ providedIn: 'root' })
export class BedApi {
  private readonly http = inject(HttpClient);

  live(): Observable<LiveSnapshot> {
    return this.http.get<LiveSnapshot>('/api/live');
  }

  historyBody(hours: number): Observable<HistoryRow<BodyData>[]> {
    return this.http.get<HistoryRow<BodyData>[]>('/api/history', { params: { event: 'body', hours } });
  }

  historyPreferences(hours: number): Observable<HistoryRow<Preferences>[]> {
    return this.http.get<HistoryRow<Preferences>[]>('/api/history', {
      params: { event: 'preferences', hours },
    });
  }

  lastSession(): Observable<SessionSummary> {
    return this.http.get<SessionSummary>('/api/analysis/last-session');
  }

  setMode(mode: string): Observable<void> {
    return this.http.post<void>('/api/control/mode', { mode });
  }

  setFirmness(value: number): Observable<void> {
    return this.http.post<void>('/api/control/firmness', { value });
  }

  setManualProfile(zones: ManualProfile): Observable<void> {
    return this.http.post<void>('/api/control/manual-profile', { zones });
  }

  fill(): Observable<void> {
    return this.http.post<void>('/api/control/fill', {});
  }

  stop(): Observable<void> {
    return this.http.post<void>('/api/control/stop', {});
  }

  setConfiguration(configuration: string): Observable<void> {
    return this.http.post<void>('/api/control/configuration', { configuration });
  }

  devices(): Observable<DiscoveredDevice[]> {
    return this.http.get<DiscoveredDevice[]>('/api/devices');
  }

  selectDevice(address: string): Observable<void> {
    return this.http.post<void>('/api/devices/select', { address });
  }
}
