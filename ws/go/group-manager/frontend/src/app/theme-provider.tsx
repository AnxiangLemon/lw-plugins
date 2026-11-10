import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
export type Theme = "dark" | "light" | "system";
const STORAGE_KEY = "lw-group-manager-theme";
interface ThemeContextValue {
  theme: Theme;
  resolvedTheme: "dark" | "light";
  setTheme: (value: Theme) => void;
}
const ThemeContext = createContext<ThemeContextValue | null>(null);
function readTheme(): Theme {
  try {
    const value = localStorage.getItem(STORAGE_KEY);
    if (value === "light" || value === "system") return value;
  } catch {
    /* 浏览器禁止存储时仍可切换当前会话主题。 */
  }
  return "dark";
}
/** 唯一主题来源。System 持续跟随系统，手动选项优先，并同步其他标签页的修改。 */
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, updateTheme] = useState<Theme>(readTheme);
  const [systemDark, setSystemDark] = useState(
    () => matchMedia("(prefers-color-scheme: dark)").matches,
  );
  const resolvedTheme =
    theme === "system" ? (systemDark ? "dark" : "light") : theme;
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const listener = () => setSystemDark(media.matches);
    media.addEventListener("change", listener);
    const storageListener = (event: StorageEvent) => {
      if (event.key === STORAGE_KEY) updateTheme(readTheme());
    };
    window.addEventListener("storage", storageListener);
    return () => {
      media.removeEventListener("change", listener);
      window.removeEventListener("storage", storageListener);
    };
  }, []);
  useLayoutEffect(() => {
    document.documentElement.classList.toggle("dark", resolvedTheme === "dark");
    document.documentElement.dataset.theme = resolvedTheme;
  }, [resolvedTheme]);
  const setTheme = useCallback((value: Theme) => {
    updateTheme(value);
    try {
      localStorage.setItem(STORAGE_KEY, value);
    } catch {
      /* 无持久存储时保留 React 状态。 */
    }
  }, []);
  const value = useMemo(
    () => ({ theme, resolvedTheme, setTheme }),
    [theme, resolvedTheme, setTheme],
  );
  return (
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
}
export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) throw new Error("useTheme 必须放在 ThemeProvider 内");
  return context;
}
