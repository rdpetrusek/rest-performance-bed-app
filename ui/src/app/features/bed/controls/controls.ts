import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  computed,
  effect,
  inject,
  signal,
  untracked,
} from '@angular/core';

import { BedStore } from '../bed-store';
import { ManualProfile, PAD_BYPASS } from '../bed-models';

type Mode = 'manual' | 'pressure' | 'position';

/** How long an edit sits untouched before it's sent and the bed fills to it. */
const APPLY_DELAY_MS = 1500;

const toProfile = (z: readonly number[]): ManualProfile => [z[0], z[1], z[2], z[3]];

@Component({
  selector: 'app-controls',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './controls.html',
})
export class Controls {
  protected readonly store = inject(BedStore);

  readonly modes: readonly { value: Mode; label: string }[] = [
    { value: 'manual', label: 'Manual' },
    { value: 'pressure', label: 'Automatic' },
    { value: 'position', label: 'Position' },
  ];

  readonly mode = signal<Mode>('manual');
  readonly firmness = signal(28);
  readonly zones = signal<ManualProfile>([26, 16, 18, 26]);
  /** True while an edit is waiting out APPLY_DELAY_MS. */
  readonly pending = signal(false);
  private applyTimer: ReturnType<typeof setTimeout> | undefined;

  readonly statusColor = computed(() => (this.store.controlError() ? '#d9534f' : ''));
  readonly status = computed(() =>
    this.pending() ? 'Waiting for you to finish, then filling…' : this.store.controlStatus(),
  );

  constructor() {
    // Mirrors the bed's own preferences whenever a live update arrives, so the
    // panel shows the bed's last-known state - except while an edit is still
    // pending or being sent, which a stale update would otherwise undo.
    effect(() => {
      const prefs = this.store.live().preferences;
      if (untracked(this.pending) || untracked(this.store.controlsInFlight) > 0) return;
      if (prefs?.mode) this.mode.set(prefs.mode as Mode);
      if (prefs?.firmness !== undefined) this.firmness.set(prefs.firmness);
      const p = prefs?.manualprofile;
      if (p?.length === 4) this.zones.set(toProfile(p));
    });
    inject(DestroyRef).onDestroy(() => clearTimeout(this.applyTimer));
  }

  /** Switches the bed's mode right away; an edit pending for the old mode is dropped. */
  selectMode(mode: Mode): void {
    this.cancelPending();
    this.mode.set(mode);
    this.store.applyMode(mode);
  }

  /** The bed's current configuration and its pad-requirement twin, if it's one the firmware knows. */
  readonly padConfig = computed(() => {
    const current = this.store.live().device?.configuration;
    const other = current ? PAD_BYPASS[current] : undefined;
    if (!current || !other) return null;
    return { current, other, bypassed: current.includes('z00') };
  });

  togglePadBypass(): void {
    const cfg = this.padConfig();
    if (cfg) this.store.setConfiguration(cfg.other);
  }

  setFirmness(value: string): void {
    this.firmness.set(this.clamp(Number(value)));
    this.schedule(() => this.store.applyFirmness(this.firmness()));
  }

  setZone(index: number, value: string): void {
    // Clamped only when sent, so typing "3" on the way to "35" isn't bumped
    // up to the minimum mid-keystroke. An empty field just restarts the wait.
    const n = Number(value);
    if (value !== '' && Number.isFinite(n)) {
      this.zones.update((z) => toProfile(z.map((v, i) => (i === index ? Math.round(n) : v))));
    }
    this.schedule(() => {
      const clamped = toProfile(this.zones().map((v) => this.clamp(v)));
      this.zones.set(clamped);
      this.store.applyManualProfile(clamped);
    });
  }

  stop(): void {
    this.cancelPending();
    this.store.stop();
  }

  /** (Re)starts the wait; apply runs once the user has left the value alone for APPLY_DELAY_MS. */
  private schedule(apply: () => void): void {
    clearTimeout(this.applyTimer);
    this.pending.set(true);
    this.applyTimer = setTimeout(() => {
      this.pending.set(false);
      apply();
    }, APPLY_DELAY_MS);
  }

  private cancelPending(): void {
    clearTimeout(this.applyTimer);
    this.pending.set(false);
  }

  /** Keeps a value inside the bed's own minimum..maximum; the server rejects anything outside it. */
  private clamp(n: number): number {
    const { min, max } = this.store.airLimits();
    return Math.min(max, Math.max(min, Number.isFinite(n) ? Math.round(n) : min));
  }
}
