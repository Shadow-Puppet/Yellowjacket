import { LitElement, html } from 'lit';
import { customElement } from 'lit/decorators.js';

const seekBar = () => html`
<progress></progress>
`;

@customElement('seek-bar')
export class SeekBar extends LitElement {

  override render() {
    return seekBar();
  }
}
