import { LitElement, html, css, type PropertyValues } from 'lit';
import { customElement, property} from 'lit/decorators.js';
import {SignalWatcher, watch, signal} from '@lit-labs/signals';
import { ref, createRef } from 'lit/directives/ref.js';
import { SlRange } from '@node_modules/@shoelace-style/shoelace/dist/shoelace';

const progress = signal(20);

@customElement('seek-bar')
export class SeekBar extends SignalWatcher(LitElement) {
  @property()
  trackLengthSeconds: number = 100;

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
    --track-color-active: yellow;
    --track-color-inactive: white;
    --track-height: 6px;
    --track-width: 50%
  }
  `;
  override render() {
    return html`
    <sl-range value="${progress.get()}" ${ref(this.rangeRef)}></sl-range>
    `;
  }

  init(trackLength: number){
    this.trackLengthSeconds = trackLength;
  }
  
  getSeekBarUpdateIntervalMillis(){
    return this.trackLengthSeconds * 10;
  }

  // update progress update interval timer thing
  // protected override update(changedProperties: PropertyValues): void {
  //     if (changedProperties.has("trackLengthSeconds")){
  //       this.stopProgress();
  //       this.startProgress();
  //     }
  //     if(changedProperties.has("isPlaying")){
  //       if(this.isPlaying){
  //         this.startProgress();
  //       }
  //       else{
  //         this.stopProgress();
  //       }
  //     }
  // }

  stopProgress(){
    clearInterval(this.timerID);
  }

  startProgress(){
    this.timerID = setInterval(this.incrementProgressValue, this.getSeekBarUpdateIntervalMillis());
  }

  async incrementProgressValue(){
    progress.set(progress.get() + 1);
  }
}

