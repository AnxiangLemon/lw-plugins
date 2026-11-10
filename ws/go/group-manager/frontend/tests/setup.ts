import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// jsdom 只提供 DOM，不包含真实浏览器布局/指针能力。这些垫片仅补齐组件交互所需
// 的浏览器接口，不拦截 fetch、不模拟群聊接口，也不向页面注入业务数据。
afterEach(() => cleanup());

class TestResizeObserver implements ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
Object.defineProperty(globalThis, "ResizeObserver", {
  configurable: true,
  writable: true,
  value: TestResizeObserver,
});

if (!globalThis.PointerEvent) {
  class TestPointerEvent extends MouseEvent {
    readonly pointerId: number;
    readonly pointerType: string;
    readonly isPrimary: boolean;
    constructor(type: string, init: PointerEventInit = {}) {
      super(type, init);
      this.pointerId = init.pointerId ?? 1;
      this.pointerType = init.pointerType ?? "mouse";
      this.isPrimary = init.isPrimary ?? true;
    }
  }
  Object.defineProperty(globalThis, "PointerEvent", {
    configurable: true,
    writable: true,
    value: TestPointerEvent,
  });
}
for (const method of [
  "setPointerCapture",
  "releasePointerCapture",
  "scrollIntoView",
] as const) {
  if (!HTMLElement.prototype[method])
    Object.defineProperty(HTMLElement.prototype, method, {
      configurable: true,
      value() {},
    });
}
if (!HTMLElement.prototype.hasPointerCapture)
  Object.defineProperty(HTMLElement.prototype, "hasPointerCapture", {
    configurable: true,
    value() {
      return false;
    },
  });
Object.defineProperty(window, "scrollTo", {
  configurable: true,
  writable: true,
  value() {},
});

Object.defineProperty(window, "matchMedia", {
  configurable: true,
  writable: true,
  value: (query: string): MediaQueryList => {
    const target = new EventTarget();
    return Object.assign(target, {
      matches: false,
      media: query,
      onchange: null,
      addListener(listener: (event: MediaQueryListEvent) => void) {
        target.addEventListener("change", listener as EventListener);
      },
      removeListener(listener: (event: MediaQueryListEvent) => void) {
        target.removeEventListener("change", listener as EventListener);
      },
    }) as MediaQueryList;
  },
});
