import { AfterViewInit, Directive, ElementRef, inject } from '@angular/core';

/** Focuses an input and selects its text as soon as it appears (inline rename). */
@Directive({
  selector: 'input[appAutofocusSelect]',
  standalone: true,
})
export class AutofocusSelectDirective implements AfterViewInit {
  private el = inject<ElementRef<HTMLInputElement>>(ElementRef).nativeElement;

  ngAfterViewInit(): void {
    setTimeout(() => {
      this.el.focus();
      this.el.select();
    });
  }
}
