import { Directive, ElementRef, HostListener, OnDestroy, inject, input } from '@angular/core';

/**
 * Dark tooltip pill (optionally with key hints, e.g. ⌘ B) shown to the right of the element.
 * Rendered into <body> with fixed positioning so scrolling containers (the sidebar) don't clip it.
 */
@Directive({
  selector: '[appTooltip]',
  standalone: true,
})
export class TooltipDirective implements OnDestroy {
  readonly appTooltip = input<string | null | undefined>('');
  readonly appTooltipKeys = input<string[]>([]);

  private host = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
  private tip: HTMLDivElement | null = null;

  @HostListener('mouseenter')
  @HostListener('focusin')
  show(): void {
    const label = this.appTooltip();
    if (!label || this.tip) return;
    const tip = document.createElement('div');
    tip.setAttribute('role', 'tooltip');
    tip.className =
      'pointer-events-none fixed z-[1200] flex items-center gap-2 whitespace-nowrap rounded-lg bg-[#1a1a1a] px-2.5 py-1.5 text-xs font-medium text-white shadow-lg ring-1 ring-white/10';
    const text = document.createElement('span');
    text.textContent = label;
    tip.appendChild(text);
    const keys = this.appTooltipKeys();
    if (keys.length) {
      const group = document.createElement('span');
      group.className = 'flex items-center gap-1';
      for (const key of keys) {
        const kbd = document.createElement('kbd');
        kbd.className = 'min-w-[20px] rounded bg-white/15 px-1.5 py-0.5 text-center font-sans text-[11px] leading-none text-white/90';
        kbd.textContent = key;
        group.appendChild(kbd);
      }
      tip.appendChild(group);
    }
    document.body.appendChild(tip);
    const r = this.host.getBoundingClientRect();
    const t = tip.getBoundingClientRect();
    tip.style.left = `${Math.round(r.right + 10)}px`;
    tip.style.top = `${Math.round(r.top + r.height / 2 - t.height / 2)}px`;
    this.tip = tip;
  }

  @HostListener('mouseleave')
  @HostListener('focusout')
  @HostListener('click')
  @HostListener('mousedown')
  hide(): void {
    this.tip?.remove();
    this.tip = null;
  }

  ngOnDestroy(): void {
    this.hide();
  }
}
