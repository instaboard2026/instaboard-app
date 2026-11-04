import { AfterViewInit, Directive, ElementRef, inject } from '@angular/core';

/** Focuses an input/textarea and selects its text as soon as it appears. */
@Directive({
  selector: 'input[appAutofocusSelect], textarea[appAutofocusSelect]',
  standalone: true,
})
export class AutofocusSelectDirective implements AfterViewInit {
  private el = inject<ElementRef<HTMLInputElement | HTMLTextAreaElement>>(ElementRef).nativeElement;

  ngAfterViewInit(): void {
    setTimeout(() => {
      this.el.focus();
      this.el.select();
    });
  }
}
