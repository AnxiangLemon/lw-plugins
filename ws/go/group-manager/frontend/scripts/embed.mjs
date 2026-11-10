import { cp, mkdir, readdir, readFile, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

// 只发布 Vite 已生成的静态资源，不读取/复制宿主配置、SQLite 或 endpoint token。
const frontend = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const source = path.join(frontend, "dist");
const target = path.resolve(frontend, "../internal/manager/web");
const expected = path.resolve(frontend, "..", "internal", "manager", "web");
if (
  target !== expected ||
  !target.startsWith(path.resolve(frontend, "..") + path.sep)
)
  throw new Error("静态资源目录越界");
const index = await readFile(path.join(source, "index.html"), "utf8");
if (!index.includes("/assets/")) throw new Error("Vite 构建入口不完整");
await mkdir(target, { recursive: true });
// 仅清理本脚本拥有的 assets 目录，保留其他源文件；路径检查先于任何递归删除。
const generatedAssets = path.resolve(target, "assets");
if (path.dirname(generatedAssets) !== target)
  throw new Error("assets 目录越界");
await rm(generatedAssets, { recursive: true, force: true });
for (const name of await readdir(source))
  await cp(path.join(source, name), path.join(target, name), {
    recursive: true,
  });
console.log("已同步前端静态资源到 Go embed 目录");
