import { LitElement, html, css, type PropertyValues } from 'lit';
import { customElement, property} from 'lit/decorators.js';
import {SignalWatcher, watch, signal} from '@lit-labs/signals';
import { ref, createRef } from 'lit/directives/ref.js';
import { SlRange } from '@node_modules/@shoelace-style/shoelace/dist/shoelace';

const progress = signal(20);

@customElement('seek-bar')
export class SeekBar extends SignalWatcher(LitElement) {
  @property()
  progressIntervalMillis: number = 1000;

  @property()
  isPlaying: boolean = true;

  timerID: number = -1;
  private rangeRef = createRef<SlRange>();

  constructor(){
    super();
    this.startProgress();
  }
  static override styles = css`
  sl-range::part(base) {
    --track-color-active: red;
    --track-color-inactive: white;
    --track-height: 6px;
  }
  `;
  override render() {
    return html`
    <sl-range value="${progress.get()}" ${ref(this.rangeRef)} @sl-change="${(event: CustomEvent) => {
      this.setProgressValue((event.target as SlRange).value);
      }}"></sl-range>
    `;
  }

  init(interval: number, playing: boolean){
    this.progressIntervalMillis = interval;
    if(playing)this.startProgress
  }

  stopProgress(){
    this.isPlaying = false;
    clearInterval(this.timerID);
  }

  startProgress(){
    this.isPlaying = true;
    this.timerID = setInterval(this.incrementProgressValue, this.progressIntervalMillis);
  }

  setProgressValue(val: number){
    if(val < 0) val = 0;
    if(val > 100) val = 100;
    progress.set(val);
  }

  setProgressInterval(intervalMillis: number){
    if(intervalMillis < 10) intervalMillis = 10;
    
  }
  async incrementProgressValue(){
    if(progress.get() <100)
    {
      progress.set(progress.get() + 1);
    }
  }
}

