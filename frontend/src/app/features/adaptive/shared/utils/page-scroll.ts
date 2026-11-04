/** Id of the rounded content panel, which is the page's scroll container whenever the app shell is shown. */
export const APP_SCROLL_ID = 'app-scroll';

export function scrollPageToTop(behavior: ScrollBehavior = 'smooth'): void {
  document.getElementById(APP_SCROLL_ID)?.scrollTo({ top: 0, behavior });
  window.scrollTo({ top: 0, behavior });
}
