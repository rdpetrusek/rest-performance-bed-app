import { ChangeDetectionStrategy, Component, computed, effect, inject, signal } from '@angular/core';

import { BedStore } from '../bed-store';
import { ManualProfile, PAD_BYPASS } from '../bed-models';

@Component({
  selector: 'app-controls',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './controls.html',
})
export class Controls {
  protected readonly store = inject(BedStore);

  readonly mode = signal<'manual' | 'pressure' | 'position'>('manual');
  readonly firmness = signal(28);
  readonly zone0 = signal(26);
  readonly zone1 = signal(16);
  readonly zone2 = signal(18);
  readonly zone3 = signal(26);

  readonly statusColor = computed(() => (this.store.controlError() ? '#d9534f' : ''));

  constructor() {
    // Mirrors the bed's own preferences whenever a live update arrives, same
    // as every control in this panel: it always shows the bed's last-known
    // state, not a pending edit.
    effect(() => {
      const prefs = this.store.live().preferences;
      if (prefs?.mode) this.mode.set(prefs.mode as 'manual' | 'pressure' | 'position');
      if (prefs?.firmness !== undefined) this.firmness.set(prefs.firmness);
    });
  }

  setMode(value: string): void {
    this.mode.set(value as 'manual' | 'pressure' | 'position');
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

  readonly zones = computed(() => [this.zone0(), this.zone1(), this.zone2(), this.zone3()]);

  setFirmness(value: string): void {
    this.firmness.set(this.clamp(Number(value)));
  }

  setZone(index: 0 | 1 | 2 | 3, input: HTMLInputElement): void {
    const n = this.clamp(Number(input.value));
    // Write back too: if the clamped value equals the old one, the signal
    // doesn't change and the [value] binding wouldn't overwrite what was typed.
    input.value = String(n);
    [this.zone0, this.zone1, this.zone2, this.zone3][index].set(n);
  }

  /** Keeps a value inside the bed's own minimum..maximum; the server rejects anything outside it. */
  private clamp(n: number): number {
    const { min, max } = this.store.airLimits();
    return Math.min(max, Math.max(min, Number.isFinite(n) ? Math.round(n) : min));
  }

  applyMode(): void {
    this.store.applyMode(this.mode());
  }

  applyFirmness(): void {
    this.store.applyFirmness(this.firmness());
  }

  applyManualProfile(): void {
    const zones: ManualProfile = [this.zone0(), this.zone1(), this.zone2(), this.zone3()];
    this.store.applyManualProfile(zones);
  }

  fill(): void {
    this.store.fill();
  }

  stop(): void {
    this.store.stop();
  }
}
