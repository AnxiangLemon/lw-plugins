// 独立外部脚本符合 Go 的严格 CSP，在首屏绘制前恢复主题，避免浅色闪烁。
try {
  var preference = localStorage.getItem("lw-group-manager-theme") || "dark";
  var dark =
    preference === "dark" ||
    (preference === "system" &&
      matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
} catch {
  document.documentElement.classList.add("dark");
}
