/** 页面只能请求本机 Go API。宿主 token 从不进入前端，也不对业务写操作自动重试。 */
export async function api<T>(
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    ...(body === undefined
      ? {}
      : {
          headers: {
            "Content-Type": "application/json",
            "X-Plugin-Request": "1",
          },
          body: JSON.stringify(body),
        }),
    signal: signal
      ? AbortSignal.any([signal, AbortSignal.timeout(20000)])
      : AbortSignal.timeout(20000),
  });
  let data: T & { error?: string };
  try {
    data = await response.json();
  } catch {
    throw new Error(`请求失败：HTTP ${response.status}`);
  }
  if (!response.ok)
    throw new Error(data.error || `请求失败：HTTP ${response.status}`);
  return data;
}
export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请求未完成，请稍后重试";
}
export function query(params: Record<string, string | number | undefined>) {
  const result = new URLSearchParams();
  for (const [key, value] of Object.entries(params))
    if (value !== undefined && value !== "") result.set(key, String(value));
  return result.toString();
}
