import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { setNonce } from "get-nonce";
import { ThemeProvider } from "@/app/theme-provider";
import { AppDataProvider } from "@/app/data-context";
import { TooltipProvider } from "@/components/ui/tooltip";
import { App } from "@/app/App";
import "@/styles/globals.css";

// Radix 的滚动锁会动态插入样式。使用 Go 本次 HTML 响应的 nonce，保持严格 CSP。
const nonce = document.querySelector<HTMLMetaElement>(
  'meta[name="csp-nonce"]',
)?.content;
if (nonce) setNonce(nonce);
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <TooltipProvider>
        <AppDataProvider>
          <App />
        </AppDataProvider>
      </TooltipProvider>
    </ThemeProvider>
  </StrictMode>,
);
