import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  afterRenderEffect,
  inject,
  viewChild,
} from '@angular/core';
import { Chart } from 'chart.js/auto';
import 'chartjs-adapter-date-fns';

import { BedStore } from '../bed-store';
import { BodyData, HistoryRow, Preferences } from '../bed-models';

interface Point {
  x: string;
  y: number;
}

@Component({
  selector: 'app-history-chart',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './history-chart.html',
})
export class HistoryChart {
  protected readonly store = inject(BedStore);
  private readonly canvasRef = viewChild.required<ElementRef<HTMLCanvasElement>>('canvas');
  private chart?: Chart<'line', Point[], string>;

  constructor() {
    afterRenderEffect(() => {
      this.render(this.store.historyBody(), this.store.historyPreferences());
    });
    inject(DestroyRef).onDestroy(() => this.chart?.destroy());
  }

  onRangeChange(hours: string): void {
    this.store.setRangeHours(Number(hours));
  }

  refresh(): void {
    this.store.refreshHistory();
  }

  private render(bodyRows: HistoryRow<BodyData>[], prefRows: HistoryRow<Preferences>[]): void {
    const ctx = this.canvasRef().nativeElement.getContext('2d');
    if (!ctx) return;

    const heartRate: Point[] = bodyRows
      .filter((r) => r.data.heartrate)
      .map((r) => ({ x: r.ts, y: r.data.heartrate! }));
    const respiration: Point[] = bodyRows
      .filter((r) => r.data.breathing?.rate)
      .map((r) => ({ x: r.ts, y: r.data.breathing!.rate! }));
    const firmness: Point[] = prefRows
      .filter((r) => r.data.firmness !== undefined)
      .map((r) => ({ x: r.ts, y: r.data.firmness! }));

    const datasets = [
      { label: 'Heart rate (bpm)', data: heartRate, borderColor: '#5ec2a8', yAxisID: 'y', tension: 0.2, pointRadius: 0 },
      { label: 'Respiration (/min)', data: respiration, borderColor: '#6aa0d8', yAxisID: 'y', tension: 0.2, pointRadius: 0 },
      { label: 'Firmness', data: firmness, borderColor: '#d8a65e', yAxisID: 'y1', tension: 0.2, pointRadius: 0 },
    ];

    if (this.chart) {
      this.chart.data.datasets = datasets;
      this.chart.update();
      return;
    }
    this.chart = new Chart<'line', Point[], string>(ctx, {
      type: 'line',
      data: { datasets },
      options: {
        parsing: false,
        scales: {
          x: { type: 'time', time: { unit: 'hour' } },
          y: { position: 'left' },
          y1: { position: 'right', grid: { drawOnChartArea: false } },
        },
        interaction: { mode: 'nearest', axis: 'x', intersect: false },
      },
    });
  }
}
