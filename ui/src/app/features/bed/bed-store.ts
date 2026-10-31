import { HttpErrorResponse } from '@angular/common/http';
import { DestroyRef, Injectable, computed, inject, signal } from '@angular/core';
import { Observable, concat } from 'rxjs';

import { BedApi } from './bed-api';
import {
  BodyData,
  DEFAULT_AIR_LIMITS,
  DiscoveredDevice,
  HistoryRow,
  LiveSnapshot,
  ManualProfile,
  PAD_BYPASS,
  Preferences,
  SessionSummary,
} from './bed-models';

const LIVE_POLL_MS = 3000;
const SESSION_POLL_MS = 60_000;
const DEVICE_POLL_MS = 15_000;

/** Live/history/control state for the one bed this dashboard talks to. */
@Injectable({ providedIn: 'root' })
export class BedStore {
  private readonly api = inject(BedApi);

  readonly live = signal<LiveSnapshot>({});
  readonly liveReachable = signal(true);

  /** The bed's own pressure limits; firmness and manual zones must stay inside them. */
  readonly airLimits = computed(() => {
    const air = this.live().air;
    if (air?.minimum === undefined || air?.maximum === undefined || air.minimum > air.maximum) {
      return DEFAULT_AIR_LIMITS;
    }
    return { min: air.minimum, max: air.maximum };
  });

  /**
   * Whether the sensor pad is connected. With a "z00" configuration the
   * firmware never reads the pad, so a missing one doesn't block air control.
   */
  readonly padStatus = computed<'connected' | 'disconnected' | 'bypassed' | 'unknown'>(() => {
    const { surface, device } = this.live();
    if (device?.configuration?.includes('z00')) return 'bypassed';
    if (!surface) return 'unknown';
    return surface.rows && surface.columns ? 'connected' : 'disconnected';
  });

  /** The bed's current configuration and its pad-requirement twin, if it's one the firmware knows. */
  readonly padConfig = computed(() => {
    const current = this.live().device?.configuration;
    const other = current ? PAD_BYPASS[current] : undefined;
    if (!current || !other) return null;
    return { current, other, bypassed: current.includes('z00') };
  });

  /** Flips the configuration to its pad-requirement twin (see padConfig). */
  togglePadBypass(): void {
    const cfg = this.padConfig();
    if (cfg) this.setConfiguration(cfg.other);
  }

  readonly session = signal<SessionSummary | null>(null);
  readonly sessionError = signal<string | null>(null);

  readonly rangeHours = signal(24);
  readonly historyBody = signal<HistoryRow<BodyData>[]>([]);
  readonly historyPreferences = signal<HistoryRow<Preferences>[]>([]);

  readonly controlStatus = signal('');
  readonly controlError = signal(false);
  /** How many control requests are still in flight; live updates shouldn't overwrite edits until it's 0. */
  readonly controlsInFlight = signal(0);

  readonly devices = signal<DiscoveredDevice[]>([]);

  constructor() {
    this.refreshLive();
    this.refreshSession();
    this.refreshHistory();
    this.refreshDevices();
    const liveTimer = setInterval(() => this.refreshLive(), LIVE_POLL_MS);
    const sessionTimer = setInterval(() => this.refreshSession(), SESSION_POLL_MS);
    const deviceTimer = setInterval(() => this.refreshDevices(), DEVICE_POLL_MS);
    inject(DestroyRef).onDestroy(() => {
      clearInterval(liveTimer);
      clearInterval(sessionTimer);
      clearInterval(deviceTimer);
    });
  }

  refreshLive(): void {
    this.api.live().subscribe({
      next: (snapshot) => {
        this.live.set(snapshot);
        this.liveReachable.set(true);
      },
      error: (err) => {
        this.liveReachable.set(false);
        console.error('refreshLive', err);
      },
    });
  }

  refreshSession(): void {
    this.api.lastSession().subscribe({
      next: (session) => {
        this.session.set(session);
        this.sessionError.set(null);
      },
      error: (err) => {
        this.sessionError.set('Error loading session summary.');
        console.error('refreshSession', err);
      },
    });
  }

  refreshHistory(): void {
    const hours = this.rangeHours();
    this.api.historyBody(hours).subscribe({
      next: (rows) => this.historyBody.set(rows),
      error: (err) => console.error('refreshHistory body', err),
    });
    this.api.historyPreferences(hours).subscribe({
      next: (rows) => this.historyPreferences.set(rows),
      error: (err) => console.error('refreshHistory preferences', err),
    });
  }

  setRangeHours(hours: number): void {
    this.rangeHours.set(hours);
    this.refreshHistory();
  }

  refreshDevices(): void {
    this.api.devices().subscribe({
      next: (devices) => this.devices.set(devices),
      error: (err) => console.error('refreshDevices', err),
    });
  }

  selectDevice(address: string): void {
    this.api.selectDevice(address).subscribe({
      next: () => {
        this.controlStatus.set(`OK: switched to ${address}`);
        this.controlError.set(false);
        this.refreshLive();
      },
      error: (err: HttpErrorResponse) => {
        this.controlStatus.set(`Failed: switch to ${address} - ${err.message}`);
        this.controlError.set(true);
      },
    });
  }

  applyMode(mode: string): void {
    this.runControl(`mode (${mode})`, this.api.setMode(mode));
  }

  /** Automatic mode at the given firmness, then fill so the bed moves to it right away. */
  applyFirmness(value: number): void {
    this.runControl(
      `firmness ${value}`,
      concat(this.api.setMode('pressure'), this.api.setFirmness(value), this.api.fill()),
    );
  }

  /** Manual mode with these zone targets, then fill - the sequence the official app sends (docs/protocol.md). */
  applyManualProfile(zones: ManualProfile): void {
    this.runControl(
      `manual profile ${zones.join('/')}`,
      concat(this.api.setMode('manual'), this.api.setManualProfile(zones), this.api.fill()),
    );
  }

  stop(): void {
    this.runControl('stop', this.api.stop());
  }

  setConfiguration(configuration: string): void {
    this.runControl(`configuration (${configuration})`, this.api.setConfiguration(configuration));
  }

  private runControl(label: string, request: Observable<void>): void {
    this.controlsInFlight.update((n) => n + 1);
    // On complete, not next: a concat()ed sequence emits once per step.
    request.subscribe({
      complete: () => {
        this.controlsInFlight.update((n) => n - 1);
        this.controlStatus.set(`OK: ${label}`);
        this.controlError.set(false);
      },
      error: (err: HttpErrorResponse) => {
        this.controlsInFlight.update((n) => n - 1);
        this.controlStatus.set(`Failed: ${label} - ${err.message}`);
        this.controlError.set(true);
      },
    });
  }
}
