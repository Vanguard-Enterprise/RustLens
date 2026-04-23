import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => {
  cleanup();
});

// Radix UI relies on these APIs which jsdom does not implement.
if (typeof window !== "undefined") {
  if (!window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
  window.HTMLElement.prototype.scrollIntoView = function () {};
  window.HTMLElement.prototype.hasPointerCapture = function () {
    return false;
  };
  window.HTMLElement.prototype.releasePointerCapture = function () {};
  window.HTMLElement.prototype.setPointerCapture = function () {};
}
