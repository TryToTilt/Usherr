// Home fork: the parts of page-flip (StPageFlip) the comic reader uses; the
// package's own type files aren't picked up by this build's resolution.
declare module 'page-flip' {
  export interface FlipSetting {
    width: number
    height: number
    size?: 'fixed' | 'stretch'
    minWidth?: number
    maxWidth?: number
    minHeight?: number
    maxHeight?: number
    showCover?: boolean
    usePortrait?: boolean
    autoSize?: boolean
    maxShadowOpacity?: number
    mobileScrollSupport?: boolean
    flippingTime?: number
    startPage?: number
    drawShadow?: boolean
  }
  export interface FlipEvent {
    data: number | string
  }
  export class PageFlip {
    constructor(element: HTMLElement, setting: FlipSetting)
    loadFromHTML(items: NodeListOf<HTMLElement> | HTMLElement[]): void
    loadFromImages(images: string[]): void
    on(event: 'flip' | 'changeOrientation' | 'changeState' | 'init' | 'update', cb: (e: FlipEvent) => void): PageFlip
    flipNext(corner?: 'top' | 'bottom'): void
    flipPrev(corner?: 'top' | 'bottom'): void
    flip(page: number, corner?: 'top' | 'bottom'): void
    turnToPage(page: number): void
    getCurrentPageIndex(): number
    getPageCount(): number
    destroy(): void
  }
}
